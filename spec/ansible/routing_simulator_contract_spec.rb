# frozen_string_literal: true

require_relative '../support/routing_simulator_inventory'

RSpec.describe 'routing simulator scenario contracts' do
  def key_verdict(scenario, &)
    changed = RoutingSimulatorInventory.changed_scenario(scenario, &)
    RoutingSimulatorInventory.scenario_verdict(changed, RoutingSimulatorInventory::KEYS, scenario)
  end

  def contract_valid?(scenario, &)
    RoutingSimulatorInventory.scenario_valid?(RoutingSimulatorInventory.changed_scenario(scenario, &), scenario)
  end

  it 'rejects a contract with a node role outside the role list' do
    changed = RoutingSimulatorInventory.changed do |copy|
      copy['routing_simulator_contracts']['native_bgp']['node_roles'] = %w[provider_router remote_client transit_router]
    end

    expect(RoutingSimulatorInventory.valid?(changed, RoutingSimulatorInventory::NODES)).to be(false)
  end

  it 'rejects an unsupported scenario kind' do
    expect(contract_valid?('astound_static') { |scenario| scenario['kind'] = 'wireguard_peer' }).to be(false)
  end

  it 'rejects an unsupported tunnel protocol' do
    expect(contract_valid?('tunnel_static') { |scenario| scenario['tunnels'][0]['protocol'] = 'wireguard' }).to be(false)
  end

  it 'rejects a session with an empty peer policy' do
    valid = contract_valid?('etheric_native') { |scenario| scenario['sessions'][0]['policy']['export_prefixes'] = [] }

    expect(valid).to be(false)
  end

  def contract_valid_without_dscp?(scenario, connection)
    changed = RoutingSimulatorInventory.changed_scenario(scenario) do |data, copy|
      copy.fetch('testbed_routing_forced_dscp').delete(connection)
      yield data
    end
    RoutingSimulatorInventory.scenario_valid?(changed, scenario)
  end

  it 'rejects a declared connection without a forced DSCP or an unchecked declaration' do
    valid = contract_valid_without_dscp?('astound_static', 'astound') do |scenario|
      scenario['unchecked_connections'] = []
      scenario['unchecked_ipv4_connections'] = [{ 'id' => 'astound', 'reason' => 'The check skips IPv4 on astound.' }]
    end

    expect(valid).to be(false)
  end

  it 'rejects an unchecked declaration with an empty or null reason', :aggregate_failures do
    expect(contract_valid?('tunnel_vps') { |scenario| scenario['unchecked_connections'][1]['reason'] = '' }).to be(false)
    expect(contract_valid?('tunnel_vps') { |scenario| scenario['unchecked_connections'][1]['reason'] = nil }).to be(false)
    expect(contract_valid?('etheric_native') { |scenario| scenario['unchecked_ipv4_connections'][0]['reason'] = nil }).to be(false)
  end

  it 'rejects an unchecked declaration for a connection the scenario does not declare', :aggregate_failures do
    unknown = { 'id' => 'sonic_2', 'reason' => 'The scenario does not use sonic_2.' }

    expect(contract_valid?('tunnel_vps') { |scenario| scenario['unchecked_connections'] << unknown }).to be(false)
    expect(contract_valid?('tunnel_vps') { |scenario| scenario['unchecked_ipv4_connections'] << unknown }).to be(false)
  end

  it 'rejects a scenario that excludes every connection' do
    excluded = { 'id' => 'astound_static_astound', 'reason' => 'The check skips the tunnel.' }

    expect(contract_valid?('astound_static') { |scenario| scenario['unchecked_connections'] << excluded }).to be(false)
  end

  it 'rejects an ordinary IPv4 connection without a forced DSCP' do
    expect(contract_valid_without_dscp?('tunnel_vps', 'astound') { |scenario| scenario['unchecked_ipv4_connections'] = [] })
      .to be(false)
  end

  it 'reports the scenario of a missing unchecked connections declaration' do
    result = key_verdict('tunnel_static') { |scenario| scenario.delete('unchecked_connections') }

    expect(result).to eq(
      valid: false, message: 'Routing scenario tunnel_static lacks required keys: scenario key unchecked_connections'
    )
  end

  it 'rejects a gateway session without a checked connection', :aggregate_failures do
    expect(contract_valid?('tunnel_vps') { |scenario| scenario['sessions'][0].delete('connection') }).to be(false)
    expect(contract_valid?('tunnel_vps') { |scenario| scenario['sessions'][0]['connection'] = 'sonic_1' }).to be(false)
  end

  it 'rejects an upstream router that two scenarios reference' do
    valid = contract_valid?('tunnel_vps') do |scenario|
      scenario['nodes'] = %w[tunnel_vps_vps tunnel_static_upstream tunnel_vps_client]
    end

    expect(valid).to be(false)
  end

  it 'reports the scenario, the session, and the key of a missing peer policy' do
    result = key_verdict('tunnel_vps') { |scenario| scenario['sessions'][0].delete('policy') }

    expect(result).to eq(
      valid: false,
      message: 'Routing scenario tunnel_vps lacks required keys: sessions entry tunnel_vps_home_sonic_1 key policy'
    )
  end

  it 'reports the scenario, the tunnel, and the key of a missing tunnel interface' do
    result = key_verdict('tunnel_static') { |scenario| scenario['tunnels'][0]['local'].delete('interface') }

    expect(result).to eq(
      valid: false,
      message: 'Routing scenario tunnel_static lacks required keys: ' \
               'tunnels entry tunnel_static_sonic_1 key local.interface'
    )
  end
end
