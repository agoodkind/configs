# frozen_string_literal: true

require 'fileutils'
require 'json'
require 'tmpdir'
require_relative '../support/command_runner'

module GuestPreparation
  REPOSITORY_ROOT = File.expand_path('../..', __dir__)
  TASK_DIRECTORY = File.join(REPOSITORY_ROOT, 'ansible', 'playbooks', 'tasks')
  FIXTURE_PLAYBOOK = File.join(REPOSITORY_ROOT, 'spec', 'fixtures', 'ansible', 'guest_preparation.yml')
  EXPECTED_REVISION = '1'
  RUN_TIMEOUT_SECONDS = 120

  class Harness
    attr_reader :revision_file, :service_file, :debug_file

    def initialize(directory)
      @directory = directory
      @revision_file = File.join(directory, 'guest', 'guest-prep-revision')
      @service_file = File.join(directory, 'service-artifact')
      @debug_file = File.join(directory, 'debug')
      ansible_directory = File.join(directory, 'ansible')
      playbook_directory = File.join(ansible_directory, 'playbooks')
      FileUtils.mkdir_p(playbook_directory)
      FileUtils.cp_r(File.join(REPOSITORY_ROOT, 'ansible', 'playbooks', '.'), playbook_directory)
      FileUtils.cp(File.join(REPOSITORY_ROOT, 'configsctl'), File.join(directory, 'configsctl'))
      FileUtils.cp(FIXTURE_PLAYBOOK, File.join(playbook_directory, 'guest-preparation.yml'))
      File.write(File.join(ansible_directory, 'inventory.ini'), "localhost ansible_connection=local\n")
      File.write(File.join(ansible_directory, 'ansible.cfg'), "[defaults]\ninventory = inventory.ini\ninterpreter_python = auto_silent\n")
    end

    def seed_revision(content)
      FileUtils.mkdir_p(File.dirname(revision_file))
      File.write(revision_file, content)
    end

    def deploy(publish: false, verify: true, check: false)
      variables = {
        'production_task_dir' => TASK_DIRECTORY,
        'guest_prep_revision' => EXPECTED_REVISION,
        'guest_prep_revision_file' => revision_file,
        'preparation_test_publish' => publish,
        'preparation_test_verify' => verify,
        'preparation_test_service_file' => service_file
      }
      argv = ['./configsctl', 'deploy', 'guest-preparation', '--extra-var', JSON.generate(variables)]
      argv << '--check' if check
      run_deploy(argv)
    end

    def deploy_debug_command
      variables = {
        'target_hosts' => 'localhost',
        'service_name' => 'adguard',
        'login_dir' => '/root',
        'repo_root' => REPOSITORY_ROOT,
        'guest_debug_command_file' => debug_file,
        'adguard_log_file' => File.join(@directory, 'adguard.log'),
        'adguard_work_dir' => File.join(@directory, 'adguard'),
        'adguard_data_dir' => File.join(@directory, 'data')
      }
      run_deploy(['./configsctl', 'deploy', 'prep-guests', '--tags', 'guest-debug', '--extra-var', JSON.generate(variables)])
    end

    def run_deploy(argv)
      result = CommandRunner.run({ 'TMPDIR' => @directory }, argv, chdir: @directory, timeout_seconds: RUN_TIMEOUT_SECONDS)
      Dir.glob(File.join(@directory, 'configs-runs', '*.log')).each do |path|
        result.output << File.read(path)
      end
      result
    end
  end
end

RSpec.describe 'the guest preparation deployment boundary' do
  around do |example|
    Dir.mktmpdir('guest-preparation') do |directory|
      @harness = GuestPreparation::Harness.new(directory)
      example.run
    end
  end

  def expect_success(result)
    expect(result.timed_out).to be(false), result.output
    expect(result.exit_status).to be_success, result.output
  end

  def expect_preparation_rejection(result)
    expect(result.timed_out).to be(false), result.output
    expect(result.exit_status).not_to be_success, result.output
    expect(result.output).to include('prep-guests'), result.output
    expect(File.exist?(@harness.service_file)).to be(false)
  end

  it 'rejects an unprepared guest before writing a service artifact' do
    expect_preparation_rejection(@harness.deploy)
    expect(File.exist?(@harness.revision_file)).to be(false)
  end

  it 'rejects a stale preparation revision before writing a service artifact' do
    @harness.seed_revision("0\n")

    expect_preparation_rejection(@harness.deploy)
    expect(File.read(@harness.revision_file)).to eq("0\n")
  end

  it 'accepts the current preparation revision after stripping whitespace' do
    @harness.seed_revision(" \n#{GuestPreparation::EXPECTED_REVISION}\n ")

    expect_success(@harness.deploy)
    expect(File.read(@harness.service_file)).to eq("service configured\n")
  end

  it 'publishes a preparation revision that permits a subsequent service artifact' do
    expect_success(@harness.deploy(publish: true))

    expect(File.read(@harness.revision_file).strip).to eq(GuestPreparation::EXPECTED_REVISION)
    expect(File.read(@harness.service_file)).to eq("service configured\n")
  end

  it 'does not publish a preparation revision in check mode' do
    expect_success(@harness.deploy(publish: true, verify: false, check: true))

    expect(File.exist?(@harness.revision_file)).to be(false)
    expect(File.exist?(@harness.service_file)).to be(false)
  end

  it 'does not replace a stale preparation revision in check mode' do
    @harness.seed_revision("0\n")

    expect_success(@harness.deploy(publish: true, verify: false, check: true))
    expect(File.read(@harness.revision_file)).to eq("0\n")
    expect(File.exist?(@harness.service_file)).to be(false)
  end

  it 'uses the inventory service when preparation targets an individual host' do
    expect_success(@harness.deploy_debug_command)

    command = File.read(@harness.debug_file)
    expect(command).to include('echo "service=adguard"', 'systemctl status AdGuardHome --no-pager')
    expect(command).to include('journalctl -u AdGuardHome')
  end
end
