# frozen_string_literal: true

require 'json'
require 'yaml'
require_relative '../support/ansible_render'
require_relative '../support/task_expressions'

# A QA deploy with tack_audit_partition_names_backfill renames the audit.events
# children outside the weekly name form before provision applies migration 017
# (TACK-551). Remove this file together with the backfill task, the production
# refusal, and the variable, which Tack removes by 2026-11-30.
module TackAuditPartitionNames
  PLAYBOOK_FILE = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks', 'deploy-tack.yml')
  GROUP_VARS_FILE = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'inventory', 'group_vars', 'tack_all.yml')
  RENAME_TASK = 'Rename the audit partitions outside the weekly name form'
  PROVISION_TASK = 'Provision (idempotent first boot; configure fresh fdb only where allowed, migrate, seed audit roles, seed product)'
  REFUSAL_TASK = 'Refuse the audit partition name backfill on a run with a production guest'
  NESTED_KEYS = %w[block rescue always].freeze
  GROUPS = { 'tack_prod_all' => ['tack.home.goodkind.io'], 'tack_qa_all' => ['tack-qa.suburban.goodkind.io'] }.freeze

  module_function

  def tasks_in(tasks)
    tasks.flat_map { |task| [task] + NESTED_KEYS.flat_map { |key| tasks_in(task[key] || []) } }
  end

  def playbook_tasks
    YAML.safe_load_file(PLAYBOOK_FILE).flat_map { |play| tasks_in(play['tasks'] || []) }
  end

  def task_named(name)
    task = playbook_tasks.find { |candidate| candidate['name'] == name }
    raise "#{PLAYBOOK_FILE} has no task named #{name.inspect}" if task.nil?

    task
  end

  # The block that contains the provision task.
  def provision_block
    playbook_tasks.find { |task| (task['block'] || []).any? { |child| child['name'] == PROVISION_TASK } }
  end

  # A registered result of the backfill with --output json, in Tack's result
  # envelope, with one rename per name in from_names.
  def backfill_result(from_names)
    renames = from_names.map { |name| { 'from' => name, 'to' => 'events_p2031_03_03' } }
    envelope = {
      '_meta' => { 'trace_id' => '0af7651916cd43dd8448eb211c80319c' },
      'result' => { 'command' => 'ops.backfill.once-audit-partition-names', 'dry_run' => false, 'result' => { 'renames' => renames } }
    }
    TaskExpressions.command_result(0, JSON.pretty_generate(envelope), '')
  end

  def conditions(variables)
    rename = task_named(RENAME_TASK)
    refusal = task_named(REFUSAL_TASK)
    TaskExpressions.evaluate(
      variables: variables,
      facts: [],
      conditions: {
        'run' => TaskExpressions.condition_list(rename['when']),
        'refusal' => TaskExpressions.condition_list(refusal.dig('ansible.builtin.assert', 'that'))
      }
    )['conditions']
  end

  def changed(result)
    rename = task_named(RENAME_TASK)
    TaskExpressions.evaluate(
      variables: { 'tack_audit_partition_names_result' => result },
      facts: [],
      conditions: { 'changed' => TaskExpressions.condition_list(rename['changed_when']) }
    )['conditions']['changed']
  end
end

RSpec.describe TackAuditPartitionNames do
  it 'renames the partitions before provision runs the migrations', :aggregate_failures do
    names = described_class.provision_block['block'].map { |task| task['name'] }
    rename = described_class.task_named(TackAuditPartitionNames::RENAME_TASK)

    expect(names.index(TackAuditPartitionNames::RENAME_TASK)).to be < names.index(TackAuditPartitionNames::PROVISION_TASK)
    expect(rename.dig('ansible.builtin.command', 'cmd')).to include('tack-ops', 'ops backfill once-audit-partition-names --execute --output json')
  end

  it 'skips the backfill by default and runs it when a QA deploy sets the flag', :aggregate_failures do
    default = YAML.safe_load_file(TackAuditPartitionNames::GROUP_VARS_FILE).fetch('tack_audit_partition_names_backfill')
    off = described_class.conditions('tack_audit_partition_names_backfill' => default, 'groups' => TackAuditPartitionNames::GROUPS,
                                     'ansible_play_hosts_all' => ['tack-qa.suburban.goodkind.io'])
    on = described_class.conditions('tack_audit_partition_names_backfill' => true, 'groups' => TackAuditPartitionNames::GROUPS,
                                    'ansible_play_hosts_all' => ['tack-qa.suburban.goodkind.io'])

    expect(off).to eq('run' => false, 'refusal' => true)
    expect(on).to eq('run' => true, 'refusal' => true)
  end

  it 'refuses the flag on a run with a production guest' do
    result = described_class.conditions('tack_audit_partition_names_backfill' => true, 'groups' => TackAuditPartitionNames::GROUPS,
                                        'ansible_play_hosts_all' => ['tack.home.goodkind.io'])

    expect(result['refusal']).to be(false)
  end

  it 'reports a change only when the backfill renamed a partition', :aggregate_failures do
    expect(described_class.changed(described_class.backfill_result(['events_tack336_proof']))).to be(true)
    expect(described_class.changed(described_class.backfill_result([]))).to be(false)
  end
end
