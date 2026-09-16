# frozen_string_literal: true

require 'fileutils'
require 'tmpdir'
require 'yaml'
require_relative '../support/ansible_render'

# The MWAN deploy validates its rendered network configuration against YANG
# modules that arrive in a git submodule. A checkout that never initialized
# that submodule carries third_party/yang as an empty directory, so the deploy
# used to reach the validation task, far past the point where it had prepared
# the guest, pushed the deploy-gate binary, and snapshotted the gateway, before
# libyang reported that it could not use its search directory. These checks
# drive the preflight over a checkout in that state and over one carrying the
# modules, and pin that it runs before the deploy prepares a guest.
module YangSchemaPreflight
  PLAYBOOKS_DIRECTORY = File.join(AnsibleRender::REPOSITORY_ROOT, 'ansible', 'playbooks')
  PREFLIGHT_TASK_FILE = File.join(PLAYBOOKS_DIRECTORY, 'tasks', 'assert-yang-schema-present.yml')
  DEPLOY_PLAYBOOK = File.join(PLAYBOOKS_DIRECTORY, 'deploy-mwan.yml')
  STACK_TASK_FILE = File.join(PLAYBOOKS_DIRECTORY, 'tasks', 'mwan-vm', 'wanconfig-stack.yml')
  FIXTURE_PLAYBOOK = 'check_yang_schema.yml'
  IMPORT_TASKS_KEY = 'ansible.builtin.import_tasks'
  IMPORT_PLAYBOOK_KEY = 'ansible.builtin.import_playbook'
  PREFLIGHT_IMPORT = 'tasks/assert-yang-schema-present.yml'
  PREP_GUESTS_IMPORT = 'prep-guests.yml'
  VALIDATE_TASK_NAME = 'Validate the rendered network configuration against the schema'
  COPY_TASK_NAME = 'Copy the gateway model and its IETF imports'
  SUBMODULE_PATH = File.join('third_party', 'yang')
  RFC_PATH = File.join(SUBMODULE_PATH, 'standard', 'ietf', 'RFC')
  MODEL_PATH = File.join('mwan', 'yang')
  MODEL_GLOB = 'goodkind-mwan-steering@*.yang'
  REPAIR_COMMAND = 'git submodule update --init third_party/yang'

  module_function

  # The modules the preflight looks for, read from its loop so a revision bump
  # moves the test with the check.
  def preflight_modules
    task = YAML.safe_load_file(PREFLIGHT_TASK_FILE).find { |candidate| candidate.key?('ansible.builtin.stat') }
    raise "#{PREFLIGHT_TASK_FILE} stats nothing" if task.nil?

    task.fetch('loop')
  end

  # The modules the deploy hands yanglint, read from the validation task's argv.
  def validated_modules
    tasks = YAML.safe_load_file(DEPLOY_PLAYBOOK, aliases: true).flat_map { |play| play['tasks'] || [] }
    task = tasks.find { |candidate| candidate['name'] == VALIDATE_TASK_NAME }
    raise "#{DEPLOY_PLAYBOOK} has no task named #{VALIDATE_TASK_NAME.inspect}" if task.nil?

    submodule_module_names(task.fetch('ansible.builtin.command').fetch('argv'))
  end

  # The modules the deploy copies onto the gateway, read from the copy loop.
  def installed_modules
    task = YAML.safe_load_file(STACK_TASK_FILE).find { |candidate| candidate['name'] == COPY_TASK_NAME }
    raise "#{STACK_TASK_FILE} has no task named #{COPY_TASK_NAME.inspect}" if task.nil?

    submodule_module_names(task.fetch('loop'))
  end

  def submodule_module_names(paths)
    paths.select { |path| path.include?(SUBMODULE_PATH) }.map { |path| File.basename(path, '.yang') }
  end

  def deploy_plays
    YAML.safe_load_file(DEPLOY_PLAYBOOK, aliases: true)
  end

  def preflight_play_index(plays)
    plays.index do |play|
      (play['tasks'] || []).any? { |task| task[IMPORT_TASKS_KEY] == PREFLIGHT_IMPORT }
    end
  end

  def prep_guests_play_index(plays)
    plays.index { |play| play[IMPORT_PLAYBOOK_KEY] == PREP_GUESTS_IMPORT }
  end

  def model_file_name
    File.basename(Dir.glob(File.join(AnsibleRender::REPOSITORY_ROOT, MODEL_PATH, MODEL_GLOB)).fetch(0))
  end

  # Builds a repository root carrying one gateway model revision and whichever
  # IETF modules the caller names, then runs the preflight over it. An empty
  # module list is the uninitialized submodule: third_party/yang exists and
  # holds nothing.
  def preflight_failure(modules)
    Dir.mktmpdir('yang-preflight') do |root|
      FileUtils.mkdir_p(File.join(root, SUBMODULE_PATH))
      FileUtils.mkdir_p(File.join(root, MODEL_PATH))
      FileUtils.touch(File.join(root, MODEL_PATH, model_file_name))
      modules.each do |name|
        FileUtils.mkdir_p(File.join(root, RFC_PATH))
        FileUtils.touch(File.join(root, RFC_PATH, "#{name}.yang"))
      end
      run_preflight(root)
    end
  end

  # Returns nil when the preflight passes and the play's output when it fails.
  def run_preflight(root)
    AnsibleRender.render(inventory: 'localhost,', playbook: FIXTURE_PLAYBOOK, extra_vars: { 'repo_root' => root })
    nil
  rescue RuntimeError => e
    e.message
  end
end

RSpec.describe YangSchemaPreflight do
  it 'looks for the modules the deploy validates against and installs' do
    looked_for = described_class.preflight_modules

    expect(looked_for).not_to be_empty
    aggregate_failures do
      expect(looked_for).to match_array(described_class.validated_modules),
                            'the preflight and the yanglint validation name different modules, ' \
                            'so a deploy can pass the preflight and still fail validation'
      expect(looked_for).to match_array(described_class.installed_modules),
                            'the preflight and the gateway copy name different modules, ' \
                            'so a deploy can pass the preflight and still fail copying them'
    end
  end

  it 'runs before the deploy prepares a guest' do
    plays = described_class.deploy_plays
    preflight = described_class.preflight_play_index(plays)
    prep_guests = described_class.prep_guests_play_index(plays)

    expect(preflight).not_to be_nil, "#{YangSchemaPreflight::DEPLOY_PLAYBOOK} imports the preflight nowhere"
    expect(prep_guests).not_to be_nil, "#{YangSchemaPreflight::DEPLOY_PLAYBOOK} prepares no guest"
    expect(preflight).to be < prep_guests,
                         "the preflight is play #{preflight} and guest preparation is play #{prep_guests}; " \
                         'a failed preflight would leave the guest already prepared'
  end

  it 'refuses a checkout whose yang submodule was never initialized' do
    output = described_class.preflight_failure([])

    expect(output).not_to be_nil, 'the preflight passed a checkout with no schema modules'
    aggregate_failures do
      described_class.preflight_modules.each do |name|
        expect(output).to include(name), "the failure names no missing module #{name}"
      end
      expect(output).to include(YangSchemaPreflight::RFC_PATH),
                        'the failure does not name the directory it looked in'
      expect(output).to include(YangSchemaPreflight::REPAIR_COMMAND),
                        'the failure does not tell the operator the command that repairs the checkout'
    end
  end

  it 'accepts a checkout carrying the pinned modules' do
    output = described_class.preflight_failure(described_class.preflight_modules)

    expect(output).to be_nil, "the preflight refused a complete checkout:\n#{output}"
  end
end
