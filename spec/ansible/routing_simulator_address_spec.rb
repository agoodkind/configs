# frozen_string_literal: true

require_relative '../support/routing_simulator_inventory'

RSpec.describe 'routing simulator addresses and identities' do
  def address_verdict(scenario, &)
    changed = RoutingSimulatorInventory.changed_scenario(scenario, &)
    RoutingSimulatorInventory.scenario_verdict(changed, RoutingSimulatorInventory::ADDRESSES, scenario)
  end

  it 'rejects a tunnel local endpoint that differs from the connection address' do
    result = address_verdict('tunnel_static') do |scenario|
      scenario['tunnels'][0]['local']['outer_ipv4'] = '{{ testbed_routing_connections.sonic_2.mwan_ipv4 }}'
    end

    expect(result).to eq(
      valid: false,
      message: 'Routing scenario tunnel_static: tunnel tunnel_static_sonic_1 local.outer_ipv4 differs from ' \
               'the mwan_ipv4 of connection sonic_1'
    )
  end

  it 'rejects a tunnel remote endpoint that another node owns' do
    result = address_verdict('tunnel_static') do |scenario, copy|
      other = copy['service_mapping']['routing_tunnel_static_upstream_suburban']['routing_interfaces']['management']
      scenario['tunnels'][0]['remote']['outer_ipv4'] = other['ipv4']
    end

    expect(result.fetch(:valid)).to be(false)
  end

  it 'rejects a session address that another node owns' do
    result = address_verdict('tunnel_vps') do |scenario|
      scenario['sessions'][2]['remote']['address'] = scenario['sessions'][2]['local']['address']
    end

    expect(result.fetch(:valid)).to be(false)
  end

  it 'rejects a multihop TTL on a directly connected session' do
    result = address_verdict('etheric_native') { |scenario| scenario['sessions'][0]['multihop_ttl'] = 2 }

    expect(result.fetch(:valid)).to be(false)
  end

  it 'rejects a multihop session with a TTL below its hop count' do
    result = address_verdict('tunnel_upstream') { |scenario| scenario['sessions'][0]['multihop_ttl'] = 1 }

    expect(result.fetch(:valid)).to be(false)
  end

  it 'rejects two interfaces with the same address and reports both owners' do
    changed = RoutingSimulatorInventory.changed do |copy|
      mapping = copy['service_mapping']
      mapping['routing_tunnel_vps_client_suburban']['routing_interfaces']['remote']['ipv6'] =
        mapping['routing_tunnel_vps_upstream_suburban']['routing_interfaces']['remote']['ipv6']
    end

    expect(RoutingSimulatorInventory.verdict(changed, RoutingSimulatorInventory::ADDRESS_OWNERS)).to eq(
      valid: false,
      message: 'The routing simulator inventory repeats an address: 2001:db8:580:1::1 belongs to ' \
               'node tunnel_vps_upstream interface remote and to node tunnel_vps_client interface remote'
    )
  end

  it 'rejects a tunnel inner address that an interface owns and reports both owners' do
    changed = RoutingSimulatorInventory.changed_scenario('tunnel_vps') do |scenario, copy|
      gateway = copy['service_mapping']['mwan_suburban']['routing_interfaces']['etheric']
      scenario['tunnels'][0]['remote']['inner_ipv6'] = gateway['ipv6']
    end

    expect(RoutingSimulatorInventory.verdict(changed, RoutingSimulatorInventory::ADDRESS_OWNERS)).to eq(
      valid: false,
      message: 'The routing simulator inventory repeats an address: 2001:db8:2ff:20::2 belongs to ' \
               'node mwan interface etheric and to tunnel tunnel_vps_sonic_1 remote.inner_ipv6'
    )
  end

  it 'rejects two tunnels with the same endpoint tuple' do
    changed = RoutingSimulatorInventory.changed_scenario('tunnel_vps') do |scenario, copy|
      static_vps = copy['service_mapping']['routing_tunnel_static_vps_suburban']
      scenario['tunnels'][0]['remote']['outer_ipv4'] = static_vps['routing_interfaces']['outer']['ipv4']
    end

    expect(RoutingSimulatorInventory.identity_valid?(changed, 'tunnel_endpoint_tuple')).to be(false)
  end

  it 'rejects two guests with the same VMID' do
    changed = RoutingSimulatorInventory.changed do |copy|
      mapping = copy['service_mapping']
      mapping['routing_tunnel_vps_client_suburban']['vmid'] = mapping['routing_tunnel_vps_upstream_suburban']['vmid']
    end

    expect(RoutingSimulatorInventory.identity_valid?(changed, 'vmid')).to be(false)
  end
end
