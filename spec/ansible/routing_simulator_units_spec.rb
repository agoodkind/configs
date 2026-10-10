# frozen_string_literal: true

require_relative '../support/routing_simulator_config'

RSpec.describe 'routing simulator network units' do
  let(:inventory) { RoutingSimulatorConfig.inventory }
  let(:home_prefix) { inventory.fetch('testbed_routing_home_prefix') }

  def render(node, template, items = {}, source = inventory)
    RoutingSimulatorConfig.render(source, node, template, items)
  end

  def address(service, interface, family)
    RoutingSimulatorConfig.interface_address(inventory, service, interface, family)
  end

  it 'renders one SIT device per declared tunnel with the inventory endpoints and MTU', :aggregate_failures do
    vps_outer = address('routing_tunnel_vps_vps_suburban', 'outer', 'ipv4')
    gateway_addresses = [address('mwan_suburban', 'sonic_1', 'ipv4'),
                         inventory.fetch('service_mapping').fetch('mwan_suburban').fetch('ipv4_astound')]

    gateway_addresses.each_with_index do |gateway_address, index|
      netdev = render('tunnel_vps_vps', 'tunnel.netdev.j2', 'rsim_tunnel' => "rsim.tunnels[#{index}]")
      expect(netdev.lines.map(&:strip)).to include('Kind=sit', 'MTUBytes=1480', "Local=#{vps_outer}",
                                                   "Remote=#{gateway_address}", 'TTL=64')
    end
  end

  it 'renders a changed tunnel MTU and outer TTL in the tunnel device' do
    changed = RoutingSimulatorInventory.changed_scenario('astound_static') do |scenario|
      scenario['tunnels'][0].merge!('mtu' => 1400, 'outer_ttl' => 32)
    end
    netdev = render('astound_static_endpoint', 'tunnel.netdev.j2', { 'rsim_tunnel' => 'rsim.tunnels[0]' }, changed)

    expect(netdev.lines.map(&:strip)).to include('MTUBytes=1400', 'TTL=32')
  end

  it 'configures the home prefix route in the tunnel unit of the endpoint without BGP' do
    network = render('astound_static_endpoint', 'tunnel.network.j2', 'rsim_tunnel' => 'rsim.tunnels[0]')

    expect(network).to match(/\[Route\]\nDestination=#{Regexp.escape(home_prefix)}\nGateway=2001:db8:1ff:1::1\n/)
  end

  it 'omits the tracked home route from the tunnel unit and configures it in the tracker service', :aggregate_failures do
    network = render('tunnel_static_vps', 'tunnel.network.j2', 'rsim_tunnel' => 'rsim.tunnels[0]')
    service = render('tunnel_static_vps', 'home-route-tracker.service.j2', 'rsim_route' => 'rsim.tracked_routes[0]')
    tracker = inventory.fetch('testbed_routing_home_route_tracker')

    expect(network).not_to include('[Route]')
    expect(service).to include(
      "routing-home-route-tracker #{home_prefix} 2001:db8:3ff:1::1 sit-sonic1 " \
      "#{tracker.fetch('interval_seconds')} #{tracker.fetch('failure_threshold')} #{tracker.fetch('route_metric')}"
    )
  end

  it 'routes outer packets to the gateway using the transport simulator', :aggregate_failures do
    gateway_address = address('mwan_suburban', 'sonic_1', 'ipv4')
    transport_outer = address('isp_sonic_suburban', 'outer', 'ipv4')
    outer = render('tunnel_static_vps', 'interface.network.j2', 'rsim_interface' => 'rsim.interfaces[0]')
    transit = render('tunnel_static_vps', 'interface.network.j2', 'rsim_interface' => 'rsim.interfaces[1]')

    expect(outer).to include("[Route]\nDestination=#{gateway_address}/32\nGateway=#{transport_outer}\n")
    expect(transit).not_to include(gateway_address)
  end

  it 'makes every management prefix unreachable on the first test interface only', :aggregate_failures do
    first = render('tunnel_vps_upstream', 'interface.network.j2', 'rsim_interface' => 'rsim.interfaces[0]')
    second = render('tunnel_vps_upstream', 'interface.network.j2', 'rsim_interface' => 'rsim.interfaces[1]')
    outer_net = RoutingSimulatorConfig.network(inventory, 'outer', 'ipv4')

    expect(first).to match(/\[RoutingPolicyRule\]\nFamily=ipv6\nTo=#{Regexp.escape(home_prefix)}\nType=unreachable\n/)
    expect(first).to match(/\[Route\]\nDestination=#{Regexp.escape(outer_net)}\nType=unreachable\n/)
    expect(second).not_to include('unreachable')
  end

  it 'forwards on routers and not on remote clients', :aggregate_failures do
    { 'tunnel_vps_vps' => '1', 'isp_sonic' => '1', 'tunnel_vps_client' => '0' }.each do |name, value|
      sysctl = render(name, 'sysctl.conf.j2')
      expect(sysctl.lines.map(&:strip)).to include("net.ipv6.conf.all.forwarding=#{value}"), name
    end
  end
end
