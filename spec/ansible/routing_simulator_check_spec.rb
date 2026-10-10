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

  it 'captures protocol 41 on both sides of the provider and the inner packet at the endpoint' do
    addresses = [{ 'ifname' => 'eth1', 'addr_info' => [{ 'local' => '198.51.100.2' }] },
                 { 'ifname' => 'eth2', 'addr_info' => [{ 'local' => '10.240.209.1' }] }]
    transport = { 'results' => [{ 'item' => 'sonic_1', 'stdout' => JSON.generate(addresses) }] }
    node_vmids = { 'isp_sonic' => 906, 'tunnel_static_vps' => 912 }
    captures = RoutingSimulatorConfig.captures(inventory, 'tunnel_static', transport, node_vmids)

    expect(captures.map { |capture| capture.first(4) }).to eq(
      [['before provider', 'tunnel_static_sonic_1', '906', 'eth2'],
       ['after provider', 'tunnel_static_sonic_1', '906', 'eth1'],
       ['return through provider', 'tunnel_static_sonic_1', '906', 'eth2'],
       ['inner at endpoint', 'tunnel_static_sonic_1', '912', 'sit-sonic1']]
    )
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
