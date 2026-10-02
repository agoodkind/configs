# frozen_string_literal: true

require 'yaml'
require_relative '../support/ansible_render'
require_relative '../support/task_expressions'

# These examples evaluate the when conditions of the ZFS ARC tasks with
# ansible-core, for a host that sets a ceiling and for a host at zero with no
# file, the managed file, and an unmanaged file at the managed path.
module ProxmoxZfsArc
  TASKS_FILE = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks', 'tasks', 'proxmox-zfs-arc.yml')
  GROUP_VARS_DIRECTORY = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'inventory', 'group_vars')
  GROUP_VARS = YAML.safe_load_file(File.join(GROUP_VARS_DIRECTORY, 'proxmox_servers.yml'))
  HEADER = GROUP_VARS.fetch('proxmox_zfs_arc_managed_header')
  WRITE = 'Configure the ZFS ARC ceiling'
  STAT = 'Check for the ZFS ARC ceiling file from an earlier ceiling'
  READ = 'Read the ZFS ARC ceiling file from an earlier ceiling'
  REMOVE = 'Remove the managed ZFS ARC ceiling file'
  PLAYBOOK_FILE = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks', 'deploy-proxmox.yml')
  VERIFY = 'Verify the ZFS ARC ceiling the module accepted'

  module_function

  def tasks
    YAML.safe_load_file(TASKS_FILE)
  end

  def conditions
    tasks.to_h { |task| [task.fetch('name'), TaskExpressions.condition_list(task['when'])] }
  end

  # Runs every task condition for one ceiling and one earlier file content.
  # A nil content is a host without the file; the read task is then skipped.
  def runs(ceiling, existing_content)
    stat = { 'stat' => { 'exists' => !existing_content.nil? } }
    existing = existing_content.nil? ? { 'skipped' => true } : { 'content' => [existing_content].pack('m0') }
    variables = GROUP_VARS.merge('proxmox_zfs_arc_max_bytes' => ceiling,
                                 'proxmox_zfs_arc_stat' => stat, 'proxmox_zfs_arc_existing' => existing)
    TaskExpressions.evaluate(variables: variables, facts: [], conditions: conditions).fetch('conditions')
  end

  # Renders the proxmox_zfs_arc_expected_c_max template from group_vars with
  # ansible-core, the way the inventory resolves it.
  def expected_c_max(variables)
    template = { 'expected' => GROUP_VARS.fetch('proxmox_zfs_arc_expected_c_max') }
    TaskExpressions.evaluate(variables: variables.except('proxmox_zfs_arc_expected_c_max'), facts: [],
                             renders: [TaskExpressions.render_task({}, template)])
                   .fetch('renders').first.fetch('expected')
  end

  def handlers
    YAML.safe_load_file(PLAYBOOK_FILE).flat_map { |play| play['handlers'] || [] }
  end

  # Evaluates the verify handler for one ceiling, one rollback value, and one
  # c_max reading. Returns whether it runs and whether it fails.
  def verify(ceiling, rollback, c_max)
    handler = handlers.find { |candidate| candidate['name'] == VERIFY }
    base = GROUP_VARS.merge('proxmox_zfs_arc_max_bytes' => ceiling, 'proxmox_zfs_arc_rollback_bytes' => rollback)
    variables = base.merge('proxmox_zfs_arc_expected_c_max' => expected_c_max(base),
                                 'proxmox_zfs_arc_c_max' => TaskExpressions.command_result(0, "#{c_max}\n", ''))
    TaskExpressions.evaluate(variables: variables, facts: [],
                             conditions: { 'runs' => TaskExpressions.condition_list(handler['when']),
                                           'fails' => TaskExpressions.condition_list(handler['failed_when']) })
                   .fetch('conditions')
  end
end

RSpec.describe ProxmoxZfsArc do
  it 'writes the managed file and reads or removes nothing for a ceiling above zero', :aggregate_failures do
    result = described_class.runs(1_073_741_824, nil)

    expect(result.fetch(ProxmoxZfsArc::WRITE)).to be(true)
    expect(result.fetch(ProxmoxZfsArc::STAT)).to be(false)
    expect(result.fetch(ProxmoxZfsArc::REMOVE)).to be(false)
  end

  it 'reads nothing and removes nothing at zero when the file is absent', :aggregate_failures do
    result = described_class.runs(0, nil)

    expect(result.fetch(ProxmoxZfsArc::STAT)).to be(true)
    expect(result.fetch(ProxmoxZfsArc::READ)).to be(false)
    expect(result.fetch(ProxmoxZfsArc::REMOVE)).to be(false)
  end

  it 'removes the managed file at zero after a ceiling was applied', :aggregate_failures do
    result = described_class.runs(0, "#{ProxmoxZfsArc::HEADER}\noptions zfs zfs_arc_max=1073741824\n")

    expect(result.fetch(ProxmoxZfsArc::WRITE)).to be(false)
    expect(result.fetch(ProxmoxZfsArc::READ)).to be(true)
    expect(result.fetch(ProxmoxZfsArc::REMOVE)).to be(true)
  end

  it 'leaves an unmanaged file at the managed path unchanged at zero', :aggregate_failures do
    result = described_class.runs(0, "options zfs zfs_arc_max=2147483648\n")

    expect(result.fetch(ProxmoxZfsArc::READ)).to be(true)
    expect(result.fetch(ProxmoxZfsArc::REMOVE)).to be(false)
  end

  it 'requires c_max to equal the ceiling when capped and the rollback value at zero', :aggregate_failures do
    expect(described_class.verify(1_073_741_824, 3_309_305_856, 1_073_741_824)).to eq('runs' => true, 'fails' => false)
    expect(described_class.verify(1_073_741_824, 3_309_305_856, 3_309_305_856)).to eq('runs' => true, 'fails' => true)
    expect(described_class.verify(0, 3_309_305_856, 3_309_305_856)).to eq('runs' => true, 'fails' => false)
    expect(described_class.verify(0, 3_309_305_856, 1_073_741_824)).to eq('runs' => true, 'fails' => true)
    expect(described_class.verify(0, 0, 1_073_741_824).fetch('runs')).to be(false)
  end
end
