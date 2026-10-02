# frozen_string_literal: true

require 'yaml'
require_relative '../support/task_expressions'

RSpec.describe 'MWAN legacy activation application gate' do
  let(:tasks) do
    path = File.join(AnsibleRender::REPOSITORY_ROOT, 'ansible/playbooks/tasks/verify-mwan-legacy-health.yml')
    YAML.safe_load_file(path)
  end

  def task(name)
    tasks.find { |entry| entry.fetch('name') == name }
  end

  def operation_record
    checks = %w[inbound-webpass-ipv4 inbound-att-ipv4].map do |identifier|
      { 'id' => identifier, 'dimension' => 'inbound_application', 'operation' => 'http',
        'target' => 'http://192.0.2.1/cf_check', 'family' => 'ipv4', 'max_age_seconds' => 180,
        'observer' => { 'kind' => 'lxc', 'vmid' => 100, 'machine_id' => 'observer', 'host_machine_id' => 'hypervisor' } }
    end
    results = checks.map do |check|
      check.except('id', 'max_age_seconds').merge(
        'check_id' => check.fetch('id'), 'availability' => 'complete', 'outcome' => 'pass',
        'observed_at' => '2026-10-02T03:00:01.093861617-07:00'
      )
    end
    results = JSON.parse(JSON.generate(results))
    { 'operation_id' => 'deploy', 'generation' => 'generation', 'status' => 'armed',
      'lease' => { 'id' => 'lease', 'phase' => 'legacy-npt-adoption', 'expires_at' => '2026-10-02T10:01:00Z' },
      'required_checks' => checks, 'results' => results }
  end

  def variables(operation, boundary)
    { 'mwan_legacy_health_status' => { 'stdout' => JSON.generate(operation) },
      'mwan_legacy_health_now' => { 'stdout' => '2026-10-02T10:00:10.000000Z' },
      'mwan_legacy_activation_time' => { 'stdout' => boundary },
      'mwan_deploy_trace_id' => 'deploy', 'mwan_operation_generation' => 'generation',
      'mwan_operation_lease_id' => 'lease', 'mwan_legacy_health_candidate' => false }
  end

  def verdict(operation, boundary: '2026-10-02T10:00:00.000000Z')
    facts = [TaskExpressions.fact_task(task('Require the complete nonempty application check set'))]
    check_task = task('Verify every required application result against its declared identity and freshness')
    operation.fetch('required_checks').each do |check|
      entry = TaskExpressions.fact_task(check_task)
      entry['vars'] = entry.fetch('vars').merge('item' => check)
      facts.push(entry)
    end
    facts.push(TaskExpressions.fact_task(task('Record the actual complete application verdict')))
    TaskExpressions.evaluate(variables: variables(operation, boundary), facts: facts).fetch('facts').fetch('mwan_legacy_health_ready')
  end

  def guards(operation)
    identity = task('Read the exact operation after candidate startup')
    lease = task('Require the lease to remain live during application verification')
    TaskExpressions.evaluate(
      variables: variables(operation, '2026-10-02T10:00:00.000000Z'), facts: [],
      renders: [TaskExpressions.render_task(identity, 'rejected' => "{{ #{identity.fetch('failed_when')} }}"),
                TaskExpressions.render_task(lease, 'live' => "{{ #{lease.fetch('ansible.builtin.assert').fetch('that').first} }}")]
    ).fetch('renders')
  end

  it 'requires post-start fresh replies from every declared observer' do
    operation = operation_record
    expect(verdict(operation)).to be(true)
    operation.fetch('results').last['observer']['machine_id'] = 'wrong-observer'
    expect(verdict(operation)).to be(false)
    operation = operation_record
    operation.fetch('results').last['outcome'] = 'fail'
    expect(verdict(operation)).to be(false)
  end

  it 'rejects absent, unknown, stale, pre-start and future observations' do
    operation = operation_record
    operation['results'] = []
    expect(verdict(operation)).to be(false)
    operation = operation_record
    operation.fetch('results').pop
    expect(verdict(operation)).to be(false)
    operation = operation_record
    operation.fetch('results').last.merge!('availability' => 'missing', 'outcome' => 'unknown')
    operation.fetch('results').last.delete('observed_at')
    expect(verdict(operation)).to be(false)
    operation = operation_record
    operation.fetch('results').last['observed_at'] = '2026-10-02T09:59:59Z'
    expect(verdict(operation)).to be(false)
    operation.fetch('results').last['observed_at'] = '2026-10-02T09:00:01Z'
    expect(verdict(operation, boundary: '2026-10-02T09:00:00.000000Z')).to be(false)
    operation.fetch('results').last['observed_at'] = '2026-10-02T10:00:11Z'
    expect(verdict(operation)).to be(false)
  end

  it 'requires the exact armed operation and a live matching lease' do
    operation = operation_record
    expect(guards(operation)).to eq([{ 'rejected' => false }, { 'live' => true }])
    operation.fetch('lease')['id'] = 'another-lease'
    expect(guards(operation).first.fetch('rejected')).to be(true)
    operation = operation_record
    operation['status'] = 'recovering'
    expect(guards(operation).first.fetch('rejected')).to be(true)
    operation = operation_record
    operation.fetch('lease')['expires_at'] = '2026-10-02T03:00:10-07:00'
    expect(guards(operation).last.fetch('live')).to be(false)
  end
end
