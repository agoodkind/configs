# frozen_string_literal: true

require 'fileutils'
require 'json'
require 'tmpdir'
require_relative '../support/routing_simulator_inventory'

RSpec.describe 'routing simulator inventory group' do
  inventory_inputs = %w[hosts service_mapping.yml group_vars/all/service_mapping.yml group_vars/all/vars.yml
                        group_vars/testbed_routing_all].freeze
  hosts = %w[suburban mwan.suburban.goodkind.io].freeze

  before(:all) do
    @work_directory = Dir.mktmpdir('routing-simulator-group')
    inventory_directory = File.join(@work_directory, 'inventory')
    @rendered_directory = File.join(@work_directory, 'rendered')
    FileUtils.mkdir_p(@rendered_directory)
    inventory_inputs.each do |input|
      destination = File.join(inventory_directory, input)
      FileUtils.mkdir_p(File.dirname(destination))
      FileUtils.cp_r(File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'inventory', input), destination)
    end
    AnsibleRender.render(
      inventory: inventory_directory, playbook: 'render_routing_inventory.yml',
      extra_vars: { 'output_directory' => @rendered_directory, 'ansible_become' => false }
    )
  end

  after(:all) do
    FileUtils.remove_entry(@work_directory)
  end

  hosts.each do |host|
    it "loads every routing scenario for #{host}", :aggregate_failures do
      rendered = JSON.parse(File.read(File.join(@rendered_directory, "#{host}.json")))
      scenario_names = RoutingSimulatorInventory.scenario_names(RoutingSimulatorInventory.inventory)

      expect(rendered.fetch('groups')).to include('testbed_routing_all')
      expect(rendered.fetch('scenarios').keys).to eq(scenario_names)
      expect(rendered.dig('scenarios', 'tunnel_vps', 'tunnels', 0, 'remote', 'outer_ipv4')).to eq(
        RoutingSimulatorInventory.inventory.dig('service_mapping', 'routing_tunnel_vps_vps_suburban',
                                                'routing_interfaces', 'outer', 'ipv4')
      )
    end
  end
end
