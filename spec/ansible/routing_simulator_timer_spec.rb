# frozen_string_literal: true

require_relative '../support/routing_simulator_inventory'

RSpec.describe 'routing simulator tunnel TTL and session timers' do
  def valid?(scenario, task_name, &)
    changed = RoutingSimulatorInventory.changed_scenario(scenario, &)
    RoutingSimulatorInventory.scenario_valid?(changed, scenario, task_name: task_name)
  end

  it 'rejects a tunnel without an outer TTL' do
    changed = RoutingSimulatorInventory.changed_scenario('tunnel_vps') do |scenario|
      scenario['tunnels'][1].delete('outer_ttl')
    end
    result = RoutingSimulatorInventory.scenario_verdict(changed, RoutingSimulatorInventory::KEYS, 'tunnel_vps')

    expect(result).to eq(
      valid: false,
      message: 'Routing scenario tunnel_vps lacks required keys: tunnels entry tunnel_vps_astound key outer_ttl'
    )
  end

  it 'rejects an outer TTL outside the IPv4 TTL range', :aggregate_failures do
    [0, 256].each do |ttl|
      valid = valid?('tunnel_static', RoutingSimulatorInventory::SCENARIOS) do |scenario|
        scenario['tunnels'][0]['outer_ttl'] = ttl
      end
      expect(valid).to be(false), "TTL #{ttl} is accepted"
    end
  end

  it 'rejects a hold time below three keepalive intervals' do
    valid = valid?('etheric_native', RoutingSimulatorInventory::ADDRESSES) do |scenario|
      scenario['sessions'][0].merge!('keepalive_seconds' => 4, 'hold_seconds' => 9)
    end

    expect(valid).to be(false)
  end
end
