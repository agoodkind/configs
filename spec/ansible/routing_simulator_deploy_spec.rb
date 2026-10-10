# frozen_string_literal: true

require_relative '../support/routing_simulator_config'

RSpec.describe 'routing simulator deploy' do
  let(:inventory) { RoutingSimulatorConfig.inventory }

  def task(name, file)
    RoutingSimulatorConfig.task(name, 'tasks', file)
  end

  def files(node)
    collect = TaskExpressions.fact_task(task('Collect the simulator files', 'deploy-routing-simulator-files.yml'))
    RoutingSimulatorConfig.evaluate_node(inventory, node, facts: [collect]).fetch('facts').fetch('rsim_files')
  end

  def pushes?(staged_checksum, guest_output)
    push = task('Push each file with a changed checksum', 'deploy-routing-simulator-push.yml')
    item = [[], { 'stat' => { 'checksum' => staged_checksum } }, { 'stdout' => guest_output }]
    TaskExpressions.evaluate(
      variables: { 'item' => item }, facts: [],
      conditions: { 'push' => TaskExpressions.condition_list(push.fetch('when')) }
    ).fetch('conditions').fetch('push')
  end

  def wait_retries(scenario, source = inventory)
    inputs = TaskExpressions.fact_task(task('Collect the fault inputs', 'check-routing-recovery-inputs.yml'))
    TaskExpressions.evaluate(
      variables: source.slice(*RoutingSimulatorConfig::LITERAL_VARIABLES).merge('routing_scenario_name' => scenario),
      facts: RoutingSimulatorConfig.inventory_facts(source) + [inputs]
    ).fetch('facts').fetch('routing_recovery').fetch('wait_retries')
  end

  it 'lists the FRR and tracker files only for a node that needs them', :aggregate_failures do
    vps_groups = files('tunnel_static_vps').map(&:last).uniq
    client_files = files('tunnel_static_client')

    expect(vps_groups).to contain_exactly('nftables', 'sysctl', 'network', 'frr', 'tracker')
    expect(files('tunnel_static_vps').map { |file| file[1] }).to include(
      '/etc/systemd/network/40-rsim-sit-sonic1.netdev', '/etc/systemd/system/routing-home-route-0.service'
    )
    expect(client_files.map(&:last).uniq).to contain_exactly('nftables', 'sysctl', 'network')
  end

  it 'pushes a file only when the guest checksum differs', :aggregate_failures do
    expect(pushes?('abc123', "abc123  /etc/nftables.conf.candidate\n")).to be(false)
    expect(pushes?('abc123', "def456  /etc/nftables.conf.candidate\n")).to be(true)
    expect(pushes?('abc123', '')).to be(true)
  end

  it 'derives the fault wait from the configured hold time', :aggregate_failures do
    checks = inventory.fetch('testbed_routing_checks')
    longer = RoutingSimulatorInventory.changed_scenario('tunnel_upstream') do |scenario|
      scenario['sessions'][0].merge!('keepalive_seconds' => 60, 'hold_seconds' => 180)
    end
    delay = checks.fetch('retry_delay_seconds')

    expect(wait_retries('tunnel_upstream')).to eq((9.0 / delay).ceil + checks.fetch('recovery_retries'))
    expect(wait_retries('tunnel_upstream', longer)).to eq((180.0 / delay).ceil + checks.fetch('recovery_retries'))
  end
end
