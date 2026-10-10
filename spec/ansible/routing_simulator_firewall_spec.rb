# frozen_string_literal: true

require_relative '../support/routing_simulator_config'

RSpec.describe 'routing simulator firewall' do
  let(:inventory) { RoutingSimulatorConfig.inventory }

  def firewall(node)
    RoutingSimulatorConfig.render(inventory, node, 'nftables.conf.j2')
  end

  it 'permits protocol 41 from the gateway endpoint and translates no packet', :aggregate_failures do
    gateway_address = inventory.fetch('service_mapping').fetch('mwan_suburban').fetch('ipv4_astound')
    endpoint_outer = RoutingSimulatorConfig.interface_address(
      inventory, 'routing_astound_static_endpoint_suburban', 'outer', 'ipv4'
    )
    rendered = firewall('astound_static_endpoint')

    expect(rendered).to include("ip saddr #{gateway_address} ip daddr #{endpoint_outer} ip protocol 41 accept")
    expect(rendered).to include('iifname { "eth1", "eth2", "sit-astound" } oifname { "eth1", "eth2", "sit-astound" } accept')
    expect(rendered).to include('iifname "eth0" drop', 'oifname "eth0" drop')
    expect(rendered).not_to match(/table ip6? nat|masquerade/)
  end

  it 'forwards no packet on a remote client and accepts the transfer port', :aggregate_failures do
    rendered = firewall('tunnel_vps_client')
    port = inventory.fetch('testbed_routing_checks').fetch('transfer_port')

    expect(rendered).not_to include('oifname {')
    expect(rendered).to include("tcp dport #{port} accept")
  end

  it 'masquerades the Sonic provider links on the uplink only', :aggregate_failures do
    rendered = firewall('isp_sonic')
    provider_nets = %w[sonic_1 sonic_2].map { |name| RoutingSimulatorConfig.network(inventory, name, 'ipv4') }
    masquerade_rules = rendered.lines.map(&:strip).grep(/masquerade/)

    expect(masquerade_rules).to eq(provider_nets.map { |net| "oifname \"eth0\" ip saddr #{net} masquerade" })
    expect(rendered).to include('iifname { "eth1", "eth2", "eth3" } oifname { "eth1", "eth2", "eth3" } accept')
    expect(rendered).to include(
      "iifname { \"eth2\", \"eth3\" } oifname \"eth0\" ip saddr { #{provider_nets.join(', ')} } accept"
    )
    expect(rendered).not_to include('table ip6 nat')
  end

  it 'masquerades no packet on the Etheric provider router' do
    expect(firewall('etheric_native_router')).not_to match(/table ip6? nat|masquerade/)
  end
end
