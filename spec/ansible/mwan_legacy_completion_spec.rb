# frozen_string_literal: true

require 'yaml'
require_relative '../support/task_expressions'

RSpec.describe 'MWAN mutation completion' do
  let(:tasks) do
    path = File.join(AnsibleRender::REPOSITORY_ROOT, 'ansible/playbooks/tasks/adopt-mwan-legacy-npt.yml')
    YAML.safe_load_file(path).find { |entry| entry.fetch('name') == 'Complete normal WAN activation under the original adoption lease' }
  end

  def completion_task(operation)
    if operation == 'Account for terminal candidate installer failures'
      path = File.join(AnsibleRender::REPOSITORY_ROOT, 'ansible/playbooks/tasks/install-mwan-core-runtime.yml')
      return YAML.safe_load_file(path).find { |entry| entry.fetch('name') == operation }.fetch('always').first
    end

    tasks.fetch('block').find { |entry| entry.fetch('name') == operation }.fetch('always').first
  end

  def pending?(operation, variable, result)
    TaskExpressions.evaluate(
      variables: { 'mwan_legacy_mutation_pending' => true, 'mwan_install_reuse_lease' => true,
                   'mwan_legacy_stop_job' => { 'ansible_job_id' => 'systemd-job' },
                   'mwan_legacy_start_job' => { 'ansible_job_id' => 'systemd-job' }, variable => result },
      facts: [TaskExpressions.fact_task(completion_task(operation))],
      renders: [TaskExpressions.render_task({}, 'pending' => '{{ mwan_legacy_mutation_pending }}')]
    ).fetch('renders').first.fetch('pending')
  end

  it 'records a completed installer failure while retaining unproved installation' do
    name = 'Account for terminal candidate installer failures'
    expect(pending?(name, 'mwan_install', { 'finished' => true, 'failed' => true, 'rc' => 1 })).to be(false)
    expect(pending?(name, 'mwan_install', { 'finished' => false, 'failed' => true })).to be(true)
    expect(pending?(name, 'mwan_install', { 'finished' => true, 'msg' => 'Timeout exceeded', 'failed' => true })).to be(true)
    expect(pending?(name, 'mwan_install', {})).to be(true)
  end

  it 'records completed command failures without accepting lost or unfinished results' do
    %w[capture adoption].each do |operation|
      variable = operation == 'capture' ? 'mwan_legacy_capture_result' : 'mwan_legacy_adopt_result'
      name = "Account for terminal legacy #{operation} failures"
      expect(pending?(name, variable, { 'finished' => true, 'failed' => true, 'rc' => 1 })).to be(false)
      expect(pending?(name, variable, { 'finished' => false, 'failed' => true })).to be(true)
      expect(pending?(name, variable, { 'finished' => true, 'msg' => 'could not find job', 'failed' => true })).to be(true)
      expect(pending?(name, variable, {})).to be(true)
    end
  end

  it 'requires terminal results from the exact systemd mutation' do
    { 'stop' => 'stopped', 'start' => 'started' }.each do |operation, state|
      name = operation == 'stop' ? 'Account for terminal legacy stop failures' : 'Account for terminal WAN startup failures'
      variable = "mwan_legacy_#{operation}_result"
      arguments = { 'name' => 'mwan-ifmgr@wan', 'state' => state }
      completed = arguments.merge('finished' => true, 'ansible_job_id' => 'systemd-job')
      expect(pending?(name, variable, completed)).to be(false)
      expect(pending?(name, variable, completed.merge('ansible_job_id' => 'another-job'))).to be(true)
      expect(pending?(name, variable, completed.merge('name' => 'unrelated.service'))).to be(true)
      expect(pending?(name, variable, completed.merge('state' => 'restarted'))).to be(true)
      result = { 'finished' => true, 'failed' => true, 'ansible_job_id' => 'systemd-job',
                 'invocation' => { 'module_args' => arguments } }
      expect(pending?(name, variable, result)).to be(false)
      result['invocation'] = { 'module_args' => { 'jid' => 'missing-job', 'mode' => 'status' } }
      expect(pending?(name, variable, result)).to be(true)
      timeout = { 'finished' => true, 'ansible_job_id' => 'systemd-job', 'failed' => true, 'msg' => 'Timeout exceeded' }
      expect(pending?(name, variable, timeout)).to be(true)
      expect(pending?(name, variable, { 'finished' => false })).to be(true)
      expect(pending?(name, variable, {})).to be(true)
    end
  end

  def transfer_pending?(file, variable, result)
    path = File.join(AnsibleRender::REPOSITORY_ROOT, "ansible/playbooks/tasks/#{file}.yml")
    completion = YAML.safe_load_file(path).find { |entry| entry.key?('always') }.fetch('always').first
    TaskExpressions.evaluate(
      variables: { 'mwan_transfer_lease_active' => true, 'mwan_transfer_mutation_pending' => true,
                   'mwan_ifmgr_restart_job' => { 'ansible_job_id' => 'systemd-job' }, variable => result },
      facts: [TaskExpressions.fact_task(completion)],
      renders: [TaskExpressions.render_task({}, 'pending' => '{{ mwan_transfer_mutation_pending }}')]
    ).fetch('renders').first.fetch('pending')
  end

  it 'releases proved handover completion while retaining lost or unfinished remote work' do
    { 'reload-mwan-networkd' => 'mwan_networkd_reload_result',
      'release-mwan-connection' => 'mwan_transfer_reconfigure_result' }.each do |file, variable|
      expect(transfer_pending?(file, variable, { 'finished' => true, 'failed' => true, 'rc' => 1 })).to be(false)
      expect(transfer_pending?(file, variable, { 'finished' => false })).to be(true)
      expect(transfer_pending?(file, variable, { 'finished' => true, 'failed' => true, 'msg' => 'could not find job' })).to be(true)
    end
    completed = { 'finished' => true, 'ansible_job_id' => 'systemd-job', 'name' => 'mwan-ifmgr@wan', 'state' => 'started' }
    expect(transfer_pending?('restart-mwan-ifmgr', 'mwan_ifmgr_restart_result', completed)).to be(false)
    expect(transfer_pending?('restart-mwan-ifmgr', 'mwan_ifmgr_restart_result', completed.merge('ansible_job_id' => 'another-job'))).to be(true)
    result = { 'finished' => true, 'failed' => true, 'ansible_job_id' => 'systemd-job',
               'invocation' => { 'module_args' => { 'name' => 'mwan-ifmgr@wan', 'state' => 'restarted' } } }
    expect(transfer_pending?('restart-mwan-ifmgr', 'mwan_ifmgr_restart_result', result)).to be(false)
    result['invocation'] = { 'module_args' => { 'jid' => 'missing-job', 'mode' => 'status' } }
    expect(transfer_pending?('restart-mwan-ifmgr', 'mwan_ifmgr_restart_result', result)).to be(true)
    expect(transfer_pending?('restart-mwan-ifmgr', 'mwan_ifmgr_restart_result', {})).to be(true)
  end
end
