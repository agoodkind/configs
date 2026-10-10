# frozen_string_literal: true

require_relative '../support/routing_simulator_config'

RSpec.describe 'routing simulator BGP configuration' do
  let(:inventory) { RoutingSimulatorConfig.inventory }
  let(:home_prefix) { inventory.fetch('testbed_routing_home_prefix') }

  def frr(node, source = inventory)
    RoutingSimulatorConfig.render(source, node, 'frr.conf.j2').lines.map(&:strip)
  end

  def restarts_frr?(node, changed_groups)
    restart = RoutingSimulatorConfig.task('Install the changed FRR configuration and restart FRR',
                                          'tasks', 'deploy-routing-simulator-services.yml')
    facts = [RoutingSimulatorConfig.fact({ 'rsim_changed_groups' => changed_groups })]
    RoutingSimulatorConfig.condition(inventory, node, restart.fetch('when'), facts: facts)
  end

  it 'restarts FRR only on a BGP node with a changed FRR file', :aggregate_failures do
    expected = { 'astound_static_endpoint' => false, 'astound_static_client' => false, 'tunnel_upstream_vps' => false,
                 'isp_sonic' => false, 'tunnel_upstream_upstream' => true, 'tunnel_vps_vps' => true }

    expected.each do |name, bgp|
      expect(restarts_frr?(name, %w[frr network])).to be(bgp), name
      expect(restarts_frr?(name, %w[network nftables])).to be(false), name
    end
  end

  it 'peers the tunnel_upstream upstream router with the gateway across the VPS', :aggregate_failures do
    remote_net = RoutingSimulatorConfig.network(inventory, 'tunnel_upstream_remote', 'ipv6')

    expect(frr('tunnel_upstream_upstream')).to include(
      'router bgp 64541', 'neighbor 2001:db8:4ff:1::1 remote-as 64512',
      'neighbor 2001:db8:4ff:1::1 ebgp-multihop 2', "network #{remote_net}",
      'neighbor 2001:db8:4ff:1::1 timers 3 9', 'network ::/0', 'ipv6 route ::/0 eth2',
      "ipv6 prefix-list PL-tunnel_upstream_home-IN permit #{home_prefix}",
      "ipv6 prefix-list PL-tunnel_upstream_home-OUT permit #{remote_net}",
      'ipv6 prefix-list PL-tunnel_upstream_home-OUT permit ::/0'
    )
  end

  it 'omits multihop on a directly connected session' do
    expect(frr('tunnel_vps_upstream').grep(/ebgp-multihop/)).to be_empty
  end

  it 'announces the home prefix from a VPS peer only as a learned route', :aggregate_failures do
    upstream = RoutingSimulatorConfig.interface_address(inventory, 'routing_tunnel_vps_upstream_suburban', 'transit', 'ipv6')
    lines = frr('tunnel_vps_vps')

    expect(lines.grep(/remote-as/)).to contain_exactly(
      'neighbor 2001:db8:5ff:1::1 remote-as 64512', 'neighbor 2001:db8:5ff:2::1 remote-as 64512',
      "neighbor #{upstream} remote-as 64531"
    )
    expect(lines.grep(/\A(network|redistribute)|default-originate/)).to be_empty
    expect(lines).to include("ipv6 prefix-list PL-tunnel_vps_upstream-OUT permit #{home_prefix}")
  end

  it 'announces the configured home prefix only from the tracked tunnel route', :aggregate_failures do
    lines = frr('tunnel_static_vps')

    expect(lines).to include('redistribute kernel route-map RM-TRACKED', 'match interface sit-sonic1',
                             "ipv6 prefix-list PL-TRACKED-sit-sonic1 permit #{home_prefix}")
    expect(lines.grep(/\Anetwork/)).to be_empty
  end

  it 'announces the home prefix from the Etheric router only as a learned route' do
    expect(frr('etheric_native_router').grep(/\A(network|redistribute)|default-originate/)).to be_empty
  end

  it 'follows a changed session policy' do
    changed = RoutingSimulatorInventory.changed_scenario('etheric_native') do |scenario|
      scenario['sessions'][1]['policy']['import_prefixes'] = ['2001:db8:280:1::/64']
    end

    expect(frr('etheric_native_upstream', changed).grep(%r{ipv6 route|network ::/0|permit ::/0})).to be_empty
  end
end
