# frozen_string_literal: true

require 'yaml'
require_relative '../support/task_expressions'

RSpec.describe 'MWAN mutation lease deadline' do
  let(:task) do
    path = File.join(AnsibleRender::REPOSITORY_ROOT, 'ansible/playbooks/tasks/mwan-operation-checkpoint.yml')
    YAML.safe_load_file(path).find { |entry| entry.fetch('name') == 'Require enough live lease time for the complete mutation' }
  end

  def permits_mutation(expiry, deadline)
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
    expect(permits_mutation('2026-10-02T02:38:21.093861617-07:00', '2026-10-02T09:38:20Z')).to be(true)
    expect(permits_mutation('2026-10-02T09:38:21Z', '2026-10-02T02:38:20.093861617-07:00')).to be(true)
    expect(permits_mutation('2026-10-02T09:38:21.1Z', '2026-10-02T02:38:21-07:00')).to be(true)
    expect(permits_mutation('2026-10-02T02:38:21.093861617-07:00', '2026-10-02T09:38:22Z')).to be(false)
    expect(permits_mutation('2026-10-02T02:38:21-07:00', '2026-10-02T09:38:21Z')).to be(false)
  end
end
