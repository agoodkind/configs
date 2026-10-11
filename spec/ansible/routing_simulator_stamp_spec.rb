# frozen_string_literal: true

require 'json'
require_relative '../support/routing_simulator_config'

RSpec.describe 'routing simulator client stamp' do
  let(:inventory) { RoutingSimulatorConfig.inventory }
  let(:stamp_variables) { inventory.slice('testbed_routing_forced_dscp', 'testbed_routing_stamp_table') }

  def stamp(scenario)
    collect = RoutingSimulatorConfig.task('Collect the client stamp', 'tasks', 'check-routing-client-stamp.yml')
    variables = inventory.slice(*RoutingSimulatorConfig::LITERAL_VARIABLES).merge(stamp_variables)
    TaskExpressions.evaluate(
      variables: variables.merge('routing_scenario_name' => scenario),
      facts: RoutingSimulatorConfig.inventory_facts(inventory) + [TaskExpressions.fact_task(collect)]
    ).fetch('facts').fetch('routing_stamp')
  end

  def rule(prefix, dscp)
    'nft add table ip6 routing_scenario_stamp && nft flush table ip6 routing_scenario_stamp ' \
      "&& nft add chain ip6 routing_scenario_stamp output '{ type filter hook output priority mangle; }' " \
      "&& nft add rule ip6 routing_scenario_stamp output ip6 daddr '{ #{prefix} }' ip6 dscp set #{dscp}"
  end

  {
    'astound_static' => ['2001:db8:180:1::/64', 47], 'etheric_native' => ['2001:db8:280:1::/64', 43],
    'tunnel_static' => ['2001:db8:380:1::/64', 51], 'tunnel_upstream' => ['2001:db8:480:1::/64', 55],
    'tunnel_vps' => ['2001:db8:580:1::/64', 59]
  }.each do |scenario, (prefix, dscp)|
    it "stamps only IPv6 packets to the #{scenario} remote prefix on the test client" do
      expect(stamp(scenario)).to eq('vmid' => 225, 'script' => rule(prefix, dscp))
    end
  end

  it 'gives every scenario connection a distinct local-use codepoint that no existing provider uses' do
    values = inventory.fetch('testbed_routing_forced_dscp').values

    expect(values.uniq).to eq(values)
    expect(values).to all(satisfy { |value| value.between?(1, 63) && value % 4 == 3 && value != 8 })
  end

  %w[check-routing-scenario-mwan.yml check-routing-recovery.yml].each do |file|
    it "stamps before the client tests and deletes the table in the always section of #{file}" do
      wrapper = RoutingSimulatorConfig.tasks('tasks', file).first
      imports = wrapper.fetch('block').map { |task| task.fetch('ansible.builtin.import_tasks') }
      restore = RoutingSimulatorConfig.tasks('tasks', 'check-routing-client-unstamp.yml').first

      expect(imports.first).to eq('check-routing-client-stamp.yml')
      expect(wrapper.fetch('always').map { |task| task.fetch('ansible.builtin.import_tasks') }).to eq(
        ['check-routing-client-unstamp.yml']
      )
      expect(restore.fetch('ansible.builtin.import_tasks')).to eq('run-routing-restore-commands.yml')
    end
  end

  it 'captures only an echo request with the client source address on the scenario path' do
    addresses = [{ 'ifname' => 'eth1', 'addr_info' => [{ 'local' => '198.51.100.2' }] },
                 { 'ifname' => 'eth2', 'addr_info' => [{ 'local' => '10.240.209.1' }] }]
    transport = { 'results' => [{ 'item' => 'sonic_1', 'stdout' => JSON.generate(addresses) }] }
    captures = RoutingSimulatorConfig.captures(inventory, 'tunnel_static', transport,
                                               { 'isp_sonic' => 906, 'tunnel_static_vps' => 912 })

    expect(captures.last).to eq(
      ['inner at endpoint', 'tunnel_static_sonic_1', '912', 'sit-sonic1',
       'icmp6 and ip6[40] == 128 and src host 3d06:bad:b01:211::225 and dst host 2001:db8:380:1::10']
    )
  end
end
