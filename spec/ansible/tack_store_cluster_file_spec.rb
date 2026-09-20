# frozen_string_literal: true

require 'tmpdir'
require 'yaml'
require_relative '../support/ansible_render'
require_relative '../support/task_expressions'

# A FoundationDB cluster file states the cluster's identity in its description
# and key, and a process given a different key joins a different cluster.
# `fdbcli coordinators` generates a new key and writes it into the cluster file
# of every connected client, and no inventory value reproduces a generated key.
# On QA on 2026-09-20 three data guests seeded from a fixed docker:docker
# literal elected their own cluster controller and ran a second cluster beside
# the real one. These checks read the real seed tasks and render the real
# environment template. The deploy's own text decides each verdict.
module TackStoreClusterFile
  PLAYBOOK_FILE = File.join(AnsibleRender::REPOSITORY_ROOT, 'ansible', 'playbooks', 'deploy-tack.yml')
  GROUP_VARS_FILE = File.join(AnsibleRender::REPOSITORY_ROOT, 'ansible', 'inventory', 'group_vars', 'tack_all.yml')
  ENV_TEMPLATE_FILE = File.join(AnsibleRender::REPOSITORY_ROOT, 'tack', 'tack.env.j2')
  RECORD_TASK_NAME = 'Record the live product store cluster file for this guest'
  READ_TASK_NAME = "Read this environment's live product store cluster file"
  FAIL_TASK_NAME = 'Fail when this environment has no live product store cluster file'
  SEED_TASK_NAME = 'Seed the product store cluster file where this guest has none'
  COPY_MODULE = 'ansible.builtin.copy'
  CONTENTS_SETTING = 'FDB_CLUSTER_FILE_CONTENTS'

  # A cluster file as a cluster writes it after a coordinator move: the
  # description, the generated key, and the coordinator the cluster runs.
  LIVE_CLUSTER_FILE = 'docker:Q7vRnT4pXz2mBw8sLd6h@[3d06:bad:b01:210::217]:4500'
  STORE_GUEST = 'tack-qa'

  # The per-environment and per-guest values the environment template reads on a
  # guest that runs no store process and no backup timers. Every other value
  # comes from the group_vars file the render play loads.
  ENVIRONMENT_VARS = {
    'tack_store_host' => '3d06:bad:b01:210::217',
    'tack_yugabyte_password' => 'render-only-ledger-login',
    'tack_meili_master_key' => 'render-only-search-login',
    'tack_audit_writer_password' => 'render-only-writer-login',
    'tack_audit_reader_password' => 'render-only-reader-login',
    'tack_audit_redactor_password' => 'render-only-redactor-login',
    'tack_audit_operator_password' => 'render-only-operator-login',
    'tack_app_password' => 'render-only-app-login',
    'tack_audit_valid_signers' => ['ed25519:0000000000000000'],
    'tack_kafka_cluster_id' => 'renderOnlyClusterId00A',
    'tack_datagen_allow_target' => 'qa',
    'tack_docker_v6_subnet' => '3d06:bad:b01:210:1::/96',
    'tack_docker_v6_gateway' => '3d06:bad:b01:210:1::1',
    'tack_backup_enabled' => false
  }.freeze

  module_function

  # Every task of every play, including the tasks of a block.
  def tasks_in(tasks)
    tasks.flat_map { |task| [task] + tasks_in(task['block'] || []) + tasks_in(task['always'] || []) }
  end

  def playbook_tasks
    plays = YAML.safe_load_file(PLAYBOOK_FILE)
    plays.flat_map { |play| tasks_in(play['tasks'] || []) }
  end

  def task_named(tasks, name)
    task = tasks.find { |candidate| candidate['name'].to_s == name }
    raise "#{PLAYBOOK_FILE} has no task named #{name.inspect}" if task.nil?

    task
  end

  # The registered result of the stat that looks for the live cluster file.
  def source_result(exists)
    { 'changed' => false, 'failed' => false, 'stat' => { 'exists' => exists } }
  end

  # The registered result of the slurp that reads it, base64 as slurp returns.
  def read_result(contents)
    { 'changed' => false, 'failed' => false, 'content' => [contents].pack('m0'), 'encoding' => 'base64' }
  end

  def seed_variables(source:, read: nil, bootstrap: false, seed: true)
    {
      'tack_store_seed_cluster_file' => seed,
      'tack_store_bootstrap' => bootstrap,
      'tack_store_cluster_file_host' => STORE_GUEST,
      'tack_store_live_cluster_file' => '',
      'tack_store_cluster_file_source' => source,
      'tack_store_cluster_file_read' => read
    }.compact
  end

  # Renders the environment file for a guest, from the cluster file the run read
  # off the guest running the store, and returns the value of one setting.
  def rendered_environment_setting(live_cluster_file, setting)
    Dir.mktmpdir('tack-env') do |output_directory|
      output_file = File.join(output_directory, '.env')
      AnsibleRender.render(
        inventory: 'localhost,',
        playbook: 'render_tack_env.yml',
        extra_vars: ENVIRONMENT_VARS.merge(
          'tack_store_live_cluster_file' => live_cluster_file,
          'group_vars_file' => GROUP_VARS_FILE,
          'template_file' => ENV_TEMPLATE_FILE,
          'output_file' => output_file
        )
      )
      setting_line = File.readlines(output_file, chomp: true).find { |line| line.start_with?("#{setting}=") }
      setting_line.to_s.delete_prefix("#{setting}=")
    end
  end
end

RSpec.describe TackStoreClusterFile do
  describe 'the cluster file the deploy seeds' do
    before(:all) do
      tasks = described_class.playbook_tasks
      @record_fact = TaskExpressions.fact_task(described_class.task_named(tasks, TackStoreClusterFile::RECORD_TASK_NAME))
      seed_task = described_class.task_named(tasks, TackStoreClusterFile::SEED_TASK_NAME)
      @seed_when = TaskExpressions.condition_list(seed_task['when'])
      @seed_render = TaskExpressions.render_task(
        seed_task, 'content' => seed_task.dig(TackStoreClusterFile::COPY_MODULE, 'content')
      )
      @read_when = TaskExpressions.condition_list(
        described_class.task_named(tasks, TackStoreClusterFile::READ_TASK_NAME)['when']
      )
      @fail_when = TaskExpressions.condition_list(
        described_class.task_named(tasks, TackStoreClusterFile::FAIL_TASK_NAME)['when']
      )
    end

    it 'writes the live bytes the run read off the guest running the store', :aggregate_failures do
      live = TackStoreClusterFile::LIVE_CLUSTER_FILE
      variables = described_class.seed_variables(
        source: described_class.source_result(true), read: described_class.read_result("#{live}\n")
      )

      result = TaskExpressions.evaluate(
        variables: variables, facts: [@record_fact],
        conditions: { 'read' => @read_when, 'seed' => @seed_when, 'fail' => @fail_when },
        renders: [@seed_render]
      )

      expect(result['facts']['tack_store_live_cluster_file']).to eq(live),
                                                                 'the run must keep the generated key of the live file'
      expect(result['renders'][0]['content']).to eq("#{live}\n")
      expect(result['conditions']).to eq('read' => true, 'seed' => true, 'fail' => false)
    end

    it 'stops the run when the guest running the store has no cluster file' do
      variables = described_class.seed_variables(source: described_class.source_result(false))

      result = TaskExpressions.evaluate(
        variables: variables, facts: [@record_fact],
        conditions: { 'read' => @read_when, 'seed' => @seed_when, 'fail' => @fail_when }
      )

      expect(result['conditions']).to eq('read' => false, 'seed' => false, 'fail' => true)
    end

    it 'seeds nothing on a from-empty environment that declares the bootstrap' do
      variables = described_class.seed_variables(source: described_class.source_result(false), bootstrap: true)

      result = TaskExpressions.evaluate(
        variables: variables, facts: [@record_fact],
        conditions: { 'read' => @read_when, 'seed' => @seed_when, 'fail' => @fail_when }
      )

      expect(result['conditions']).to eq('read' => false, 'seed' => false, 'fail' => false)
    end

    # Production leaves the seed off. Ansible skips the stat on that run, and
    # the registered result then includes no stat at all. Every condition list
    # reads the flag first and stops on it.
    it 'skips the seed where the environment has not turned it on' do
      variables = described_class.seed_variables(
        source: { 'changed' => false, 'skipped' => true }, seed: false
      )

      result = TaskExpressions.evaluate(
        variables: variables, facts: [@record_fact],
        conditions: { 'read' => @read_when, 'seed' => @seed_when, 'fail' => @fail_when }
      )

      expect(result['conditions']).to eq('read' => false, 'seed' => false, 'fail' => false)
    end
  end

  describe 'the cluster file in the rendered environment' do
    it 'repeats the live bytes for the container' do
      live = TackStoreClusterFile::LIVE_CLUSTER_FILE

      rendered = described_class.rendered_environment_setting(live, TackStoreClusterFile::CONTENTS_SETTING)

      expect(rendered).to eq(live)
    end

    it 'stays empty where the run read no cluster file' do
      rendered = described_class.rendered_environment_setting('', TackStoreClusterFile::CONTENTS_SETTING)

      expect(rendered).to eq('')
    end
  end
end
