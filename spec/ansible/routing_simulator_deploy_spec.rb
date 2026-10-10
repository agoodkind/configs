# frozen_string_literal: true

require_relative '../support/routing_simulator_config'

RSpec.describe 'routing simulator file deploy' do
  let(:inventory) { RoutingSimulatorConfig.inventory }

  def task(name, file)
    RoutingSimulatorConfig.task(name, 'tasks', file)
  end

  def files(node)
    collect = TaskExpressions.fact_task(task('Collect the simulator files', 'routing-simulator-file-list.yml'))
    RoutingSimulatorConfig.evaluate_node(inventory, node, facts: [collect]).fetch('facts').fetch('rsim_files')
  end

  def sysctl_argv(node)
    collect = TaskExpressions.fact_task(task('Collect the simulator files', 'routing-simulator-file-list.yml'))
    apply = task('Apply the simulator sysctl values', 'deploy-routing-simulator-services.yml')
    argv = RoutingSimulatorConfig.fact({ 'sysctl_argv' => apply.fetch('ansible.builtin.command').fetch('argv') })
    RoutingSimulatorConfig.evaluate_node(inventory, node, facts: [collect, argv]).fetch('facts').fetch('sysctl_argv')
  end

  def pushes?(staged_checksum, guest_output)
    push = task('Push each file with a changed checksum', 'deploy-routing-simulator-push.yml')
    item = [[], { 'stat' => { 'checksum' => staged_checksum } }, { 'stdout' => guest_output }]
    TaskExpressions.evaluate(
      variables: { 'item' => item }, facts: [],
      conditions: { 'push' => TaskExpressions.condition_list(push.fetch('when')) }
    ).fetch('conditions').fetch('push')
  end

  # pushed maps a service group to the changed value of one push result.
  def changed_groups(pushed, removed_unit:)
    record = task('Record the service groups with a changed file', 'deploy-routing-simulator-files.yml')
    results = pushed.map { |group, changed| { 'changed' => changed, 'item' => [['name', '/path', '0644', group]] } }
    TaskExpressions.evaluate(
      variables: { 'rsim_push' => { 'results' => results }, 'rsim_removed_units' => { 'changed' => removed_unit } },
      facts: [TaskExpressions.fact_task(record)]
    ).fetch('facts').fetch('rsim_changed_groups')
  end

  it 'lists the FRR and tracker files only for a node that needs them', :aggregate_failures do
    vps_files = files('tunnel_static_vps')

    expect(vps_files.map(&:last).uniq).to contain_exactly('nftables', 'sysctl', 'network', 'frr', 'tracker')
    expect(vps_files.map { |file| file[1] }).to include(
      '/etc/systemd/network/40-rsim-sit-sonic1.netdev', '/etc/systemd/system/routing-home-route-0.service'
    )
    expect(files('tunnel_static_client').map(&:last).uniq).to contain_exactly('nftables', 'sysctl', 'network')
  end

  it 'loads only the pushed simulator sysctl file in the guest' do
    vmid = inventory.fetch('service_mapping').fetch(inventory.fetch('testbed_routing_nodes')
      .fetch('tunnel_static_vps').fetch('service')).fetch('vmid').to_s
    sysctl_path = files('tunnel_static_vps').find { |file| file.last == 'sysctl' }[1]

    expect(sysctl_argv('tunnel_static_vps')).to eq(['pct', 'exec', vmid, '--', 'sysctl', '-p', sysctl_path])
  end

  it 'pushes a file only when the guest checksum differs', :aggregate_failures do
    expect(pushes?('abc123', "abc123  /etc/nftables.conf.candidate\n")).to be(false)
    expect(pushes?('abc123', "def456  /etc/nftables.conf.candidate\n")).to be(true)
    expect(pushes?('abc123', '')).to be(true)
  end

  it 'reports only the service groups of pushed files and removed units', :aggregate_failures do
    pushed = { 'nftables' => true, 'frr' => false, 'network' => false, 'tracker' => true }

    expect(changed_groups(pushed, removed_unit: false)).to contain_exactly('nftables', 'tracker')
    expect(changed_groups(pushed, removed_unit: true)).to contain_exactly('nftables', 'tracker', 'network')
    expect(changed_groups(pushed.transform_values { false }, removed_unit: false)).to eq([])
  end
end
