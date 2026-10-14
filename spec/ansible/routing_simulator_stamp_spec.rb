# frozen_string_literal: true

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
end
