# frozen_string_literal: true

require 'yaml'
require_relative '../support/task_expressions'

RSpec.describe 'MWAN legacy mutation completion' do
  let(:tasks) do
    path = File.join(AnsibleRender::REPOSITORY_ROOT, 'ansible/playbooks/tasks/adopt-mwan-legacy-npt.yml')
    YAML.safe_load_file(path).find { |entry| entry.fetch('name') == 'Complete normal WAN activation under the original adoption lease' }
  end

  def completion_task(operation)
    tasks.fetch('block').find { |entry| entry.fetch('name') == operation }.fetch('always').first
  end

  def pending?(operation, variable, result)
    TaskExpressions.evaluate(
      variables: { 'mwan_legacy_mutation_pending' => true, variable => result },
      facts: [TaskExpressions.fact_task(completion_task(operation))],
      renders: [TaskExpressions.render_task({}, 'pending' => '{{ mwan_legacy_mutation_pending }}')]
    ).fetch('renders').first.fetch('pending')
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
      result = { 'finished' => true, 'failed' => true, 'invocation' => { 'module_args' => arguments } }
      expect(pending?(name, variable, result)).to be(false)
      result['invocation'] = { 'module_args' => { 'jid' => 'missing-job', 'mode' => 'status' } }
      expect(pending?(name, variable, result)).to be(true)
      expect(pending?(name, variable, { 'finished' => false })).to be(true)
      expect(pending?(name, variable, {})).to be(true)
    end
  end
end
