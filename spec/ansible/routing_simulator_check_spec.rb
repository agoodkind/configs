# frozen_string_literal: true

require 'json'
require_relative '../support/routing_simulator_config'

RSpec.describe 'routing simulator checks' do
  let(:inventory) { RoutingSimulatorConfig.inventory }
  let(:infrastructure_results) do
    ['node tunnel_static_vps', 'node tunnel_static_upstream', 'node tunnel_static_client', 'node isp_sonic',
     'outer transport tunnel_static_sonic_1', 'session tunnel_static_upstream', 'remote client gateway',
     'transport source tunnel_static_sonic_1']
  end

  def required_checks_valid?(mode, results)
    RoutingSimulatorConfig.required_checks_valid?(inventory, 'tunnel_static', mode, results)
  end

  it 'accepts a complete infrastructure run' do
    expect(required_checks_valid?('infrastructure', infrastructure_results)).to be(true)
  end

  it 'rejects an infrastructure run without a node check' do
    expect(required_checks_valid?('infrastructure', infrastructure_results - ['node isp_sonic'])).to be(false)
  end

  it 'rejects an infrastructure run without the Sonic source address check' do
    results = infrastructure_results - ['transport source tunnel_static_sonic_1']

    expect(required_checks_valid?('infrastructure', results)).to be(false)
  end

  it 'requires no transport source check on the Astound connection' do
    results = ['node astound_static_endpoint', 'node astound_static_client',
               'outer transport astound_static_astound', 'remote client gateway']

    expect(RoutingSimulatorConfig.required_checks_valid?(inventory, 'astound_static', 'infrastructure', results)).to be(true)
  end

  def unreachable?(prefix, routes, rules)
    assertion = RoutingSimulatorConfig.task('Require the unreachable management prefixes',
                                            'tasks', 'check-routing-simulator-node.yml')
    results = [routes, rules].map { |stdout| TaskExpressions.command_result(0, stdout, '') }
    variables = { 'item' => prefix, 'rsim_unreachable' => { 'results' => results } }
    RoutingSimulatorConfig.assertion_valid?(inventory, assertion, variables, [])
  end

  it 'accepts the unreachable rule that systemd-networkd installs with the main table', :aggregate_failures do
    home_prefix = inventory.fetch('testbed_routing_home_prefix')
    outer_net = RoutingSimulatorConfig.network(inventory, 'outer', 'ipv4')
    routes = "unreachable #{outer_net} proto static metric 65000"
    rules = "0:\tfrom all lookup local\n32766:\tfrom all lookup main\n" \
            "32800:\tfrom all to #{home_prefix} lookup main unreachable proto static"

    expect(unreachable?(home_prefix, routes, rules)).to be(true)
    expect(unreachable?(home_prefix, routes, "32800:\tfrom all to #{home_prefix} unreachable")).to be(true)
    expect(unreachable?(outer_net, routes, rules)).to be(true)
  end

  it 'rejects a guest without the unreachable rule or route', :aggregate_failures do
    home_prefix = inventory.fetch('testbed_routing_home_prefix')
    outer_net = RoutingSimulatorConfig.network(inventory, 'outer', 'ipv4')
    rules = "32766:\tfrom all lookup main\n32800:\tfrom all to #{home_prefix} lookup 100"

    expect(unreachable?(home_prefix, "unreachable #{outer_net}", rules)).to be(false)
    expect(unreachable?(outer_net, '', rules)).to be(false)
  end

  def addresses_present?(node, addresses)
    assertion = RoutingSimulatorConfig.task('Require the declared interface and tunnel addresses',
                                            'tasks', 'check-routing-simulator-node.yml')
    node_facts = RoutingSimulatorConfig.tasks('tasks', 'routing-simulator-node-facts.yml')
                                       .map { |task| TaskExpressions.fact_task(task) }
    stdout = JSON.generate([{ 'addr_info' => addresses.map { |address| { 'local' => address } } }])
    variables = { 'rsim_name' => node, 'rsim_addresses' => TaskExpressions.command_result(0, stdout, '') }
    RoutingSimulatorConfig.assertion_valid?(inventory, assertion, variables, node_facts)
  end

  it 'requires the link-local address of each tunnel on the guest', :aggregate_failures do
    interfaces = inventory.fetch('service_mapping').fetch('routing_tunnel_vps_vps_suburban').fetch('routing_interfaces')
    interface_addresses = interfaces.except('management').values
                                    .flat_map { |interface| interface.values_at('ipv4', 'ipv6') }.reject(&:empty?)
    tunnel_addresses = interface_addresses + ['2001:db8:5ff:1::2', '2001:db8:5ff:2::2']
    link_locals = ['fe80::c633:6428:af0:d102', 'fe80::c633:6428:af0:cf02']

    expect(addresses_present?('tunnel_vps_vps', tunnel_addresses + link_locals)).to be(true)
    expect(addresses_present?('tunnel_vps_vps', tunnel_addresses + link_locals.first(1))).to be(false)
  end

  def session_established?(file, name, result, summary)
    wait = RoutingSimulatorConfig.task(name, 'tasks', file)
    session = { 'local' => { 'address' => '2001:db8:2ff:10::1' }, 'remote' => { 'address' => '2001:db8:2ff:10::2' } }
    stdout = JSON.generate(summary)
    TaskExpressions.evaluate(
      variables: { 'item' => session, result => TaskExpressions.command_result(0, stdout, '') }, facts: [],
      conditions: { 'established' => TaskExpressions.condition_list(wait.fetch('until')) }
    ).fetch('conditions').fetch('established')
  end

  {
    'check-routing-scenario-infrastructure.yml' =>
      ['Wait for each session between two simulator nodes', 'routing_remote_summary', '2001:db8:2ff:10::2'],
    'check-routing-mwan-sessions.yml' =>
      ['Wait for each gateway session on the remote peer', 'routing_gateway_summary', '2001:db8:2ff:10::1']
  }.each do |file, (name, result, peer)|
    it "reads the session state from the FRR address family summary in #{file}", :aggregate_failures do
      summary = { 'routerId' => '192.0.2.12', 'as' => 64_520, 'peers' => { peer => { 'state' => 'Established' } } }
      idle = summary.merge('peers' => { peer => { 'state' => 'Active' } })

      expect(session_established?(file, name, result, summary)).to be(true)
      expect(session_established?(file, name, result, idle)).to be(false)
      expect(session_established?(file, name, result, summary.merge('peers' => {}))).to be(false)
      expect(session_established?(file, name, result, {})).to be(false)
    end
  end

  it 'captures protocol 41 on both sides of the provider and the inner packet at the endpoint' do
    addresses = [{ 'ifname' => 'eth1', 'addr_info' => [{ 'local' => '198.51.100.2' }] },
                 { 'ifname' => 'eth2', 'addr_info' => [{ 'local' => '10.240.209.1' }] }]
    transport = { 'results' => [{ 'item' => 'sonic_1', 'stdout' => JSON.generate(addresses) }] }
    node_vmids = { 'isp_sonic' => 906, 'tunnel_static_vps' => 912 }
    captures = RoutingSimulatorConfig.captures(inventory, 'tunnel_static', 'tunnel_static_sonic_1', transport, node_vmids)

    expect(captures.map { |capture| capture.first(4) }).to eq(
      [['before provider', 'tunnel_static_sonic_1', '906', 'eth2'],
       ['after provider', 'tunnel_static_sonic_1', '906', 'eth1'],
       ['return through provider', 'tunnel_static_sonic_1', '906', 'eth2'],
       ['inner at endpoint', 'tunnel_static_sonic_1', '912', 'sit-sonic1']]
    )
  end

  it 'captures outbound packets on the connection tunnel and return packets on every tunnel to the endpoint' do
    address = ->(service, interface) { RoutingSimulatorConfig.interface_address(inventory, service, interface, 'ipv4') }
    sonic = [{ 'ifname' => 'eth1', 'addr_info' => [{ 'local' => address.call('isp_sonic_suburban', 'outer') }] },
             { 'ifname' => 'eth2', 'addr_info' => [{ 'local' => address.call('isp_sonic_suburban', 'sonic_1') }] }]
    astound = [{ 'ifname' => 'eth3', 'addr_info' => [{ 'local' => address.call('isp_astound_suburban', 'outer') }] },
               { 'ifname' => 'eth4',
                 'addr_info' => [{ 'local' => inventory.fetch('service_mapping').fetch('isp_astound_suburban').fetch('ipv4') }] }]
    transport = { 'results' => [{ 'item' => 'sonic_1', 'stdout' => JSON.generate(sonic) },
                                { 'item' => 'astound', 'stdout' => JSON.generate(astound) }] }
    node_vmids = { 'isp_sonic' => 906, 'isp_astound' => 905, 'tunnel_vps_vps' => 915 }
    captures = RoutingSimulatorConfig.captures(inventory, 'tunnel_vps', 'tunnel_vps_astound', transport, node_vmids)

    expect(captures.map { |capture| capture.first(4) }).to eq(
      [['before provider', 'tunnel_vps_astound', '905', 'eth4'],
       ['after provider', 'tunnel_vps_astound', '905', 'eth3'],
       ['return through provider', 'tunnel_vps_sonic_1', '906', 'eth2'],
       ['return through provider', 'tunnel_vps_astound', '905', 'eth4'],
       ['inner at endpoint', 'tunnel_vps_astound', '915', 'sit-astound']]
    )
  end

  it 'checks each connection that forwards the scenario IPv6 traffic with its forced DSCP', :aggregate_failures do
    dscp = inventory.fetch('testbed_routing_forced_dscp')
    {
      'astound_static' => %w[astound_static_astound], 'etheric_native' => %w[etheric],
      'tunnel_static' => %w[tunnel_static_sonic_1], 'tunnel_upstream' => %w[tunnel_upstream_sonic_2],
      'tunnel_vps' => %w[tunnel_vps_sonic_1 tunnel_vps_astound]
    }.each do |scenario, connections|
      connections.each do |connection|
        facts = RoutingSimulatorConfig.connection_facts(inventory, scenario, connection)
        expect(facts.fetch('routing_check').fetch('checked_connections')).to eq(connections)
        expect(facts.fetch('routing_connection').fetch('dscp').to_i).to eq(dscp.fetch(connection))
      end
    end
  end

  it 'binds the gateway sessions, the underlay, and the MTU to the connection', :aggregate_failures do
    astound = RoutingSimulatorConfig.connection_facts(inventory, 'tunnel_vps', 'tunnel_vps_astound').fetch('routing_connection')
    etheric = RoutingSimulatorConfig.connection_facts(inventory, 'etheric_native', 'etheric').fetch('routing_connection')

    expect(astound.fetch('gateway_sessions').map { |session| session.fetch('id') }).to eq(['tunnel_vps_home_astound'])
    expect(astound.values_at('underlay', 'mtu').map(&:to_s)).to eq(%w[astound 1480])
    expect(etheric.fetch('gateway_sessions').map { |session| session.fetch('id') }).to eq(['etheric_native_home'])
    expect(etheric.values_at('underlay', 'mtu').map(&:to_s)).to eq(%w[etheric 1500])
  end

  def tunnel_vps_mwan_results
    connection_checks = ['ordinary ipv4 before', 'packet captures', 'icmp both directions',
                         'tcp transfer both directions', 'packet size', 'ordinary ipv4 after', 'home route on upstream']
    ['node tunnel_vps_vps', 'node tunnel_vps_upstream', 'node tunnel_vps_client', 'node isp_sonic',
     'outer transport tunnel_vps_sonic_1', 'outer transport tunnel_vps_astound', 'session tunnel_vps_upstream',
     'remote client gateway', 'transport source tunnel_vps_sonic_1',
     'connection tunnel_vps_sonic_1 gateway session tunnel_vps_home_sonic_1',
     'connection tunnel_vps_astound gateway session tunnel_vps_home_astound'] +
      %w[tunnel_vps_sonic_1 tunnel_vps_astound].product(connection_checks).map { |id, check| "connection #{id} #{check}" }
  end

  it 'requires the MWAN checks of every checked connection', :aggregate_failures do
    valid = ->(results) { RoutingSimulatorConfig.required_checks_valid?(inventory, 'tunnel_vps', 'mwan', results) }

    expect(valid.call(tunnel_vps_mwan_results)).to be(true)
    expect(valid.call(tunnel_vps_mwan_results - ['connection tunnel_vps_astound packet captures'])).to be(false)
    expect(valid.call(tunnel_vps_mwan_results - ['connection tunnel_vps_astound gateway session tunnel_vps_home_astound']))
      .to be(false)
  end

  it 'derives the fault wait from the configured hold time', :aggregate_failures do
    checks = inventory.fetch('testbed_routing_checks')
    longer = RoutingSimulatorInventory.changed_scenario('tunnel_upstream') do |scenario|
      scenario['sessions'][0].merge!('keepalive_seconds' => 60, 'hold_seconds' => 180)
    end
    delay = checks.fetch('retry_delay_seconds')
    margin = checks.fetch('recovery_retries')

    expect(RoutingSimulatorConfig.fault_wait_retries(inventory, 'tunnel_upstream')).to eq((9.0 / delay).ceil + margin)
    expect(RoutingSimulatorConfig.fault_wait_retries(longer, 'tunnel_upstream')).to eq((180.0 / delay).ceil + margin)
  end

  it 'selects the tracker fault targets only for a configured home route', :aggregate_failures do
    home_prefix = inventory.fetch('testbed_routing_home_prefix')
    tracked = RoutingSimulatorConfig.fault_facts(inventory, 'tunnel_static').fetch('routing_recovery_tracked_routes')
    learned = RoutingSimulatorConfig.fault_facts(inventory, 'tunnel_vps').fetch('routing_recovery_tracked_routes')

    expect(tracked).to eq(
      [['tunnel_static_vps', home_prefix, '2001:db8:3ff:1::1', 'sit-sonic1', 'routing-home-route-0.service']]
    )
    expect(learned).to eq([])
  end

  it 'rejects an MWAN run with only the infrastructure results' do
    expect(required_checks_valid?('mwan', infrastructure_results)).to be(false)
  end

  {
    %w[all infrastructure] => true, %w[tunnel_vps mwan] => true, %w[all mwan] => false,
    %w[tunnel_vps production] => false, %w[missing infrastructure] => false
  }.each do |(scenario, mode), expected|
    it "returns #{expected} for scenario #{scenario} in mode #{mode}" do
      expect(RoutingSimulatorConfig.request_valid?(inventory, scenario, mode)).to be(expected)
    end
  end
end
