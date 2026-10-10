# frozen_string_literal: true

require 'tmpdir'
require_relative '../support/ansible_render'

RSpec.describe 'routing simulator restore commands' do
  failing_command = 'false'
  password_file = File.join(AnsibleRender::FIXTURE_DIRECTORY, 'unused-vault-password.txt')

  def restore(password_file, first_command)
    Dir.mktmpdir('routing-restore') do |directory|
      restored = File.join(directory, 'restored')
      probed = File.join(directory, 'probed')
      extra_vars = {
        'routing_restore_commands' => [first_command, "mkdir #{restored}"],
        'routing_recovery_probe_command' => "mkdir #{probed}",
        'testbed_routing_checks' => { 'restore_retries' => 2, 'retry_delay_seconds' => 0 }
      }
      result = AnsibleRender.run_playbook(password_file, 'localhost,', 'run_routing_restore_commands.yml', extra_vars,
                                          AnsibleRender::PLAYBOOK_TIMEOUT_SECONDS)
      { result: result, restored: Dir.exist?(restored), probed: Dir.exist?(probed) }
    end
  end

  it 'runs the later restore command and reports the failed command without the probe', :aggregate_failures do
    outcome = restore(password_file, failing_command)

    expect(outcome.fetch(:result).exit_status).not_to be_success, outcome.fetch(:result).output
    expect(outcome.fetch(:restored)).to be(true)
    expect(outcome.fetch(:result).output).to include("Failed restore commands: ['#{failing_command}']")
    expect(outcome.fetch(:probed)).to be(false)
  end

  it 'runs the recovery probe after every restore command succeeds', :aggregate_failures do
    outcome = restore(password_file, 'true')

    expect(outcome.fetch(:result).exit_status).to be_success, outcome.fetch(:result).output
    expect(outcome.fetch(:restored)).to be(true)
    expect(outcome.fetch(:probed)).to be(true)
  end
end
