# frozen_string_literal: true

require 'yaml'
require_relative '../support/ansible_render'
require_relative '../support/task_expressions'

# Remove this file by 2026-11-30 with the deploy-tack task "Rename the audit
# partitions outside the weekly name form" (TACK-551).
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

  def provision_block
    playbook_tasks.find { |task| (task['block'] || []).any? { |child| child['name'] == PROVISION_TASK } }
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
end
