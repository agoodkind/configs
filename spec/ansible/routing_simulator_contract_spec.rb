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
