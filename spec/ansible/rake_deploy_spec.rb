# frozen_string_literal: true

require 'fileutils'
require 'tmpdir'
require_relative '../support/command_runner'

# Every documented deploy path runs `./configsctl deploy`, which gates the
# playbook's files through the input-default linter, redacts vault values, and
# writes a run log. The rake shortcuts the deploy skill documents are one of
# those paths. These checks run the real rake tasks against a copy of the
# repository's directory layout, with a recording stand-in for the configsctl
# script at the root and a failing stand-in for every ansible entry point first
# on PATH. A task that shelled out to ansible-playbook, the way these tasks used
# to, records an ansible call and records no configsctl call.
module RakeDeploy
  REPOSITORY_ROOT = File.expand_path('../..', __dir__)
  FIXTURE_DIRECTORY = File.join(REPOSITORY_ROOT, 'spec', 'fixtures', 'rake_deploy')
  FAKE_CONFIGSCTL = 'fake-configsctl.sh'
  FAKE_ANSIBLE = 'fake-ansible.sh'

  # Every CLI the repository rules ban a deploy path from invoking.
  ANSIBLE_ENTRY_POINTS = %w[ansible ansible-playbook ansible-vault ansible-inventory ansible-console].freeze

  # The files a copied tree needs for the rake tasks under test to load.
  COPIED_FILES = [
    File.join('ansible', 'Rakefile'),
    File.join('traefik', 'Rakefile'),
    File.join('lib', 'rake_common.rb')
  ].freeze

  CONFIGSCTL_NAME = 'configsctl'
  ARGV_FILE = 'configsctl-argv'
  ANSIBLE_LOG = 'ansible-calls'
  FAKE_MODE = 0o700
  RUN_TIMEOUT_SECONDS = 120

  # Each documented rake shortcut, the directory it runs from, and the argument
  # vector configsctl must receive. The playbook stem matters: configsctl
  # resolves a stem to ansible/playbooks/<stem>.yml for the lint gate and to
  # playbooks/<stem>.yml for the play, while a bare path resolves only for the
  # play and silently skips the gate.
  CASES = [
    { task: 'deploy:mwan[vault]', directory: 'ansible',
      want: %w[deploy deploy-mwan --limit vault] },
    { task: 'deploy:proxmox', directory: 'ansible',
      want: %w[deploy deploy-proxmox] },
    { task: 'deploy:failover[mwan_failover_suburban_servers]', directory: 'ansible',
      want: %w[deploy deploy-mwan-failover --limit mwan_failover_suburban_servers] },
    { task: 'check:opnsense[opnsense_suburban_servers]', directory: 'ansible',
      want: %w[deploy deploy-opnsense --limit opnsense_suburban_servers --check --diff] },
    { task: 'check:testbed', directory: 'ansible',
      want: %w[deploy deploy-testbed --check --diff] },
    { task: 'deploy', directory: 'traefik',
      want: %w[deploy deploy-proxy] }
  ].freeze

  # One run's copied tree: the real Rakefiles, a recording configsctl at the
  # root, and a failing ansible on PATH.
  class Harness
    def initialize(root)
      @root = root
      @fake_bin = File.join(root, 'bin')
      FileUtils.mkdir_p([@fake_bin] + COPIED_FILES.map { |relative| File.join(root, File.dirname(relative)) })
      COPIED_FILES.each do |relative|
        FileUtils.cp(File.join(REPOSITORY_ROOT, relative), File.join(root, relative))
      end
      install(FAKE_CONFIGSCTL, File.join(root, CONFIGSCTL_NAME))
      ANSIBLE_ENTRY_POINTS.each { |name| install(FAKE_ANSIBLE, File.join(@fake_bin, name)) }
    end

    def install(fixture, target)
      FileUtils.cp(File.join(FIXTURE_DIRECTORY, fixture), target)
      File.chmod(FAKE_MODE, target)
    end

    def run(task, directory)
      argv = [
        'env',
        "PATH=#{@fake_bin}#{File::PATH_SEPARATOR}#{ENV.fetch('PATH')}",
        "RAKE_DEPLOY_ANSIBLE_LOG=#{ansible_log_path}",
        'rake',
        task
      ]
      CommandRunner.run({}, argv, chdir: File.join(@root, directory), timeout_seconds: RUN_TIMEOUT_SECONDS)
    end

    # The arguments the task handed configsctl, or nil when it called none.
    def configsctl_argv
      path = File.join(@root, ARGV_FILE)
      return nil unless File.exist?(path)

      File.read(path).split("\n")
    end

    def ansible_calls
      return '' unless File.exist?(ansible_log_path)

      File.read(ansible_log_path)
    end

    def ansible_log_path
      File.join(@root, ANSIBLE_LOG)
    end
  end
end

RSpec.describe 'the rake deploy shortcuts' do
  RakeDeploy::CASES.each do |scenario|
    context "with rake #{scenario[:task]} in #{scenario[:directory]}/" do
      before(:context) do
        @work_directory = Dir.mktmpdir('rake-deploy')
        @harness = RakeDeploy::Harness.new(@work_directory)
        @result = @harness.run(scenario[:task], scenario[:directory])
      end

      after(:context) do
        FileUtils.remove_entry(@work_directory)
      end

      it 'runs configsctl deploy with the documented arguments' do
        expect(@result.timed_out).to be(false), @result.output
        expect(@result.exit_status).to be_success, @result.output
        expect(@harness.configsctl_argv).to eq(scenario[:want]), @result.output
      end

      it 'invokes no ansible entry point' do
        expect(@harness.ansible_calls).to eq('')
      end
    end
  end
end
