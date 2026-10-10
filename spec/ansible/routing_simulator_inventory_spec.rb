# frozen_string_literal: true

require_relative '../support/routing_simulator_inventory'

RSpec.describe RoutingSimulatorInventory do
  let(:inventory) { described_class.inventory }

  def astound_addresses(service_mapping)
    hypervisor = described_class.group_vars('suburban_servers.yml').slice('testbed_isp_lxcs')
    gateway = described_class.group_vars('mwan_suburban_servers.yml').slice('mwan_astound_ipv4')
    shared = inventory.slice('testbed_routing_connections')
    rendered = TaskExpressions.evaluate(
      variables: { 'service_mapping' => service_mapping },
      facts: [described_class.fact(hypervisor.merge(gateway, shared))]
    ).fetch('facts')
    astound = rendered.fetch('testbed_isp_lxcs').find { |provider| provider.fetch('name') == 'astound' }
    {
      reservation: astound.fetch('v4_reservations').first.fetch('addr'),
      gateway: rendered.fetch('mwan_astound_ipv4'),
      connection: rendered.fetch('testbed_routing_connections').fetch('astound').fetch('mwan_ipv4')
    }
  end

  it 'accepts the repository inventory', :aggregate_failures do
    [described_class::NODES, described_class::MANAGEMENT, described_class::NETWORKS].each do |task_name|
      expect(described_class.valid?(inventory, task_name)).to be(true), "#{task_name} fails"
    end
    described_class.identity_names.each do |identity|
      expect(described_class.identity_valid?(inventory, identity)).to be(true), "identity #{identity} is rejected"
    end
    described_class.scenario_names(inventory).each do |scenario|
      described_class::SCENARIO_TASKS.each do |task_name|
        result = described_class.scenario_verdict(inventory, task_name, scenario)
        expect(result.fetch(:valid)).to be(true), "#{scenario}: #{result.fetch(:message)}"
      end
    end
  end

  it 'rejects an IPv6 default route on the management interfaces' do
    changed = described_class.changed { |copy| copy['testbed_routing_management']['ipv6_default_route'] = true }

    expect(described_class.valid?(changed, described_class::MANAGEMENT)).to be(false)
  end

  it 'renders the Astound reservation, gateway address, and connection from one address' do
    mapping = described_class.changed { |copy| copy['service_mapping']['mwan_suburban']['ipv4_astound'] = '10.240.207.9' }

    expect(astound_addresses(mapping.fetch('service_mapping'))).to eq(
      reservation: '10.240.207.9', gateway: '10.240.207.9/24', connection: '10.240.207.9'
    )
  end
end
