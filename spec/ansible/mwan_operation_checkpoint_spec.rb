# frozen_string_literal: true

require 'yaml'
require_relative '../support/task_expressions'

RSpec.describe 'MWAN mutation checkpoints' do
  let(:task) do
    path = File.join(AnsibleRender::REPOSITORY_ROOT, 'ansible/playbooks/tasks/mwan-operation-checkpoint.yml')
    YAML.safe_load_file(path).find { |entry| entry.fetch('name') == 'Require enough live lease time for the complete mutation' }
  end
  let(:tasks) do
    path = File.join(AnsibleRender::REPOSITORY_ROOT, 'ansible/playbooks/tasks/mwan-operation-checkpoint.yml')
    YAML.safe_load_file(path)
  end

  def permits_mutation?(expiry, deadline)
    operation = { 'operation_id' => 'deploy', 'generation' => 'generation', 'status' => 'armed',
                  'lease' => { 'id' => 'lease', 'phase' => 'prepare', 'expires_at' => expiry } }
    variables = { 'mwan_operation_checkpoint_status' => { 'stdout' => JSON.generate(operation) },
                  'mwan_operation_reuse_deadline' => { 'stdout' => deadline },
                  'mwan_deploy_trace_id' => 'deploy', 'mwan_operation_generation' => 'generation',
                  'mwan_operation_lease_id' => 'lease', 'mwan_operation_phase' => 'prepare' }
    templates = task.fetch('ansible.builtin.assert').fetch('that').each_with_index.to_h do |expression, index|
      [index.to_s, "{{ #{expression} }}"]
    end
    result = TaskExpressions.evaluate(variables: variables, facts: [], renders: [TaskExpressions.render_task(task, templates)])
    result.fetch('renders').first.values.all?(true)
  end

  it 'compares UTC and offset timestamps as instants with fractional seconds' do
    expect(permits_mutation?('2026-10-02T02:38:21.093861617-07:00', '2026-10-02T09:38:20Z')).to be(true)
    expect(permits_mutation?('2026-10-02T09:38:21Z', '2026-10-02T02:38:20.093861617-07:00')).to be(true)
    expect(permits_mutation?('2026-10-02T09:38:21.1Z', '2026-10-02T02:38:21-07:00')).to be(true)
    expect(permits_mutation?('2026-10-02T02:38:21.093861617-07:00', '2026-10-02T09:38:22Z')).to be(false)
    expect(permits_mutation?('2026-10-02T02:38:21-07:00', '2026-10-02T09:38:21Z')).to be(false)
  end

  def renders_assertion?(name, variables)
    task = tasks.find { |entry| entry.fetch('name') == name }
    templates = task.fetch('ansible.builtin.assert').fetch('that').each_with_index.to_h do |expression, index|
      [index.to_s, "{{ #{expression} }}"]
    end
    result = TaskExpressions.evaluate(variables: variables, facts: [], renders: [TaskExpressions.render_task(task, templates)])
    result.fetch('renders').first.values.all?(true)
  end

  it 'reads the exact issued lease in Recovering without authorizing another mutation' do
    operation = { 'operation_id' => 'deploy', 'generation' => 'generation', 'status' => 'recovering',
                  'lease' => { 'id' => 'lease', 'phase' => 'legacy-npt-adoption' } }
    variables = { 'mwan_operation_checkpoint_status' => { 'stdout' => JSON.generate(operation) },
                  'mwan_deploy_trace_id' => 'deploy', 'mwan_operation_generation' => 'generation',
                  'mwan_operation_lease_id' => 'lease', 'mwan_operation_phase' => 'legacy-npt-adoption',
                  'mwan_operation_acquire' => false, 'mwan_operation_reuse_lease' => true }
    expect(renders_assertion?('Require the exact operation before releasing completed work', variables)).to be(true)
    expect(renders_assertion?('Require the exact lease before reading issued remote completion', variables)).to be(true)
    expect(renders_assertion?('Reject additional writes after deployment recovery starts', variables)).to be(false)
    variables['mwan_operation_lease_id'] = 'another-lease'
    expect(renders_assertion?('Require the exact lease before reading issued remote completion', variables)).to be(false)
  end

  it 'requires the complete actual systemd watch identity from one property read' do
    operation = { 'watch' => { 'invocation_id' => 'invocation', 'pid' => 123 } }
    variables = { 'mwan_operation_checkpoint_status' => { 'stdout' => JSON.generate(operation) },
                  'mwan_operation_reuse_watch' => { 'stdout_lines' => ['MainPID=123', 'InvocationID=invocation', 'ActiveState=active'] } }
    name = 'Require the same running watch invocation and process'
    expect(renders_assertion?(name, variables)).to be(true)
    variables['mwan_operation_reuse_watch']['stdout_lines'] = ['MainPID=123', 'ActiveState=active']
    expect(renders_assertion?(name, variables)).to be(false)
    variables['mwan_operation_reuse_watch']['stdout_lines'] = ['MainPID=124', 'InvocationID=invocation', 'ActiveState=active']
    expect(renders_assertion?(name, variables)).to be(false)
  end
end
