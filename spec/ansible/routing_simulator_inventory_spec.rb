# frozen_string_literal: true

require 'yaml'
require_relative '../support/task_expressions'

# The spec evaluates the validate-routing-simulators.yml assertions with ansible-core.
module RoutingSimulatorInventory
  GROUP_VARS_DIRECTORY = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'inventory', 'group_vars')
  TASKS_FILE = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks', 'tasks', 'validate-routing-simulators.yml')
  ASSERT_KEY = 'ansible.builtin.assert'
  KEYS = 'Require every routing scenario key'
  NODES = 'Require supported routing simulator nodes and existing references'
  MANAGEMENT = 'Require management interfaces outside test forwarding'
  NETWORKS = 'Require declared routing simulator networks'
  IDENTITIES = 'Require unique routing simulator identities'
  SCENARIOS = 'Require each routing scenario to satisfy the contract of its kind'
  ADDRESSES = 'Require each tunnel and session address to match its node and link'
  SCENARIO_TASKS = [KEYS, SCENARIOS, ADDRESSES].freeze
  SHARED_VARIABLES = %w[
    testbed_routing_home_prefix testbed_routing_management_unreachable_prefixes testbed_routing_connections
    testbed_routing_nodes testbed_routing_scenarios
  ].freeze
  LITERAL_VARIABLES = %w[
    service_mapping testbed_routing_networks testbed_routing_guests testbed_routing_management
  ].freeze

  module_function

  def group_vars(name)
    YAML.safe_load_file(File.join(GROUP_VARS_DIRECTORY, name), aliases: true)
  end

  def inventory
    literal = group_vars(File.join('all', 'service_mapping.yml')).slice(*LITERAL_VARIABLES)
    literal.merge(group_vars('mwan_testbed_all.yml').slice(*SHARED_VARIABLES))
  end

  def tasks
    YAML.safe_load_file(TASKS_FILE)
  end

  def fact(values, task_vars = {})
    { 'when' => [], 'vars' => task_vars, 'set_fact' => values }
  end

  def item_fact(key, source)
    fact({ 'item' => { 'key' => key, 'value' => "{{ #{source}['#{key}'] }}" } })
  end

  def verdict(inventory, task_name, item: nil)
    position = tasks.index { |task| task['name'] == task_name }
    assertion = tasks.fetch(position)
    task_vars = TaskExpressions.value_map(assertion['vars'])
    earlier_facts = tasks.first(position).select { |task| task.key?(TaskExpressions::SET_FACT_KEY) }
    facts = SHARED_VARIABLES.map { |name| fact({ name => inventory.fetch(name) }) }
    facts += earlier_facts.map { |task| TaskExpressions.fact_task(task) }
    facts << item unless item.nil?
    facts << fact(task_vars, task_vars)
    result = TaskExpressions.evaluate(
      variables: inventory.slice(*LITERAL_VARIABLES), facts: facts,
      conditions: { 'valid' => TaskExpressions.condition_list(assertion.fetch(ASSERT_KEY).fetch('that')) },
      renders: [{ 'vars' => {}, 'templates' => { 'message' => assertion.fetch(ASSERT_KEY).fetch('fail_msg') } }]
    )
    { valid: result.fetch('conditions').fetch('valid'), message: result.fetch('renders').first.fetch('message') }
  end

  def valid?(inventory, task_name)
    verdict(inventory, task_name).fetch(:valid)
  end

  def scenario_verdict(inventory, task_name, scenario)
    verdict(inventory, task_name, item: item_fact(scenario, 'testbed_routing_scenarios'))
  end

  def scenario_valid?(inventory, scenario, task_name: SCENARIOS)
    scenario_verdict(inventory, task_name, scenario).fetch(:valid)
  end

  def identity_valid?(inventory, identity)
    verdict(inventory, IDENTITIES, item: item_fact(identity, 'routing_simulator_identities')).fetch(:valid)
  end

  def identity_names
    collect = tasks.find { |task| task['name'] == 'Collect the routing simulator identities' }
    collect.fetch(TaskExpressions::SET_FACT_KEY).fetch('routing_simulator_identities').keys
  end

  def changed
    copy = Marshal.load(Marshal.dump(inventory))
    yield copy
    copy
  end

  def astound_addresses(service_mapping)
    hypervisor = group_vars('suburban_servers.yml').slice('testbed_isp_lxcs')
    gateway = group_vars('mwan_suburban_servers.yml').slice('mwan_astound_ipv4')
    shared = group_vars('mwan_testbed_all.yml').slice('testbed_routing_connections')
    rendered = TaskExpressions.evaluate(
      variables: { 'service_mapping' => service_mapping }, facts: [fact(hypervisor.merge(gateway, shared))]
    ).fetch('facts')
    astound = rendered.fetch('testbed_isp_lxcs').find { |provider| provider.fetch('name') == 'astound' }
    {
      reservation: astound.fetch('v4_reservations').first.fetch('addr'),
      gateway: rendered.fetch('mwan_astound_ipv4'),
      connection: rendered.fetch('testbed_routing_connections').fetch('astound').fetch('mwan_ipv4')
    }
  end
end

RSpec.describe RoutingSimulatorInventory do
  let(:inventory) { described_class.inventory }

  def change_scenario(name)
    described_class.changed { |copy| yield copy['testbed_routing_scenarios'][name], copy }
  end

  it 'accepts the repository inventory', :aggregate_failures do
    [described_class::NODES, described_class::MANAGEMENT, described_class::NETWORKS].each do |task_name|
      expect(described_class.valid?(inventory, task_name)).to be(true), "#{task_name} fails"
    end
    described_class.identity_names.each do |identity|
      expect(described_class.identity_valid?(inventory, identity)).to be(true), "identity #{identity} is rejected"
    end
    inventory.fetch('testbed_routing_scenarios').each_key do |scenario|
      described_class::SCENARIO_TASKS.each do |task_name|
        result = described_class.scenario_verdict(inventory, task_name, scenario)
        expect(result.fetch(:valid)).to be(true), "#{scenario}: #{result.fetch(:message)}"
      end
    end
  end

  it 'rejects an unsupported scenario kind' do
    changed = change_scenario('astound_static') { |scenario| scenario['kind'] = 'wireguard_peer' }

    expect(described_class.scenario_valid?(changed, 'astound_static')).to be(false)
  end

  it 'rejects an unsupported tunnel protocol' do
    changed = change_scenario('tunnel_static') { |scenario| scenario['tunnels'][0]['protocol'] = 'wireguard' }

    expect(described_class.scenario_valid?(changed, 'tunnel_static')).to be(false)
  end

  it 'reports the scenario, the session, and the key of a missing peer policy', :aggregate_failures do
    changed = change_scenario('tunnel_vps') { |scenario| scenario['sessions'][0].delete('policy') }
    result = described_class.scenario_verdict(changed, described_class::KEYS, 'tunnel_vps')

    expect(result.fetch(:valid)).to be(false)
    expect(result.fetch(:message)).to eq(
      'Routing scenario tunnel_vps lacks required keys: sessions entry tunnel_vps_home_sonic_1 key policy'
    )
  end

  it 'reports the scenario, the tunnel, and the key of a missing tunnel interface', :aggregate_failures do
    changed = change_scenario('tunnel_static') { |scenario| scenario['tunnels'][0]['local'].delete('interface') }
    result = described_class.scenario_verdict(changed, described_class::KEYS, 'tunnel_static')

    expect(result.fetch(:valid)).to be(false)
    expect(result.fetch(:message)).to eq(
      'Routing scenario tunnel_static lacks required keys: tunnels entry tunnel_static_sonic_1 key local.interface'
    )
  end

  it 'rejects a session with an empty peer policy' do
    changed = change_scenario('etheric_native') { |scenario| scenario['sessions'][0]['policy']['export_prefixes'] = [] }

    expect(described_class.scenario_valid?(changed, 'etheric_native')).to be(false)
  end

  it 'rejects a tunnel local endpoint that differs from the connection address' do
    changed = change_scenario('tunnel_static') do |scenario|
      scenario['tunnels'][0]['local']['outer_ipv4'] = '{{ testbed_routing_connections.sonic_2.mwan_ipv4 }}'
    end
    result = described_class.scenario_verdict(changed, described_class::ADDRESSES, 'tunnel_static')

    expect(result).to eq(
      valid: false,
      message: 'Routing scenario tunnel_static: tunnel tunnel_static_sonic_1 local.outer_ipv4 differs from ' \
               'the mwan_ipv4 of connection sonic_1'
    )
  end

  it 'rejects a tunnel remote endpoint that another node owns' do
    changed = change_scenario('tunnel_static') do |scenario, copy|
      other = copy['service_mapping']['routing_tunnel_static_upstream_suburban']['routing_interfaces']['management']
      scenario['tunnels'][0]['remote']['outer_ipv4'] = other['ipv4']
    end

    expect(described_class.scenario_valid?(changed, 'tunnel_static', task_name: described_class::ADDRESSES)).to be(false)
  end

  it 'rejects a session address that another node owns' do
    changed = change_scenario('tunnel_vps') do |scenario|
      scenario['sessions'][2]['remote']['address'] = scenario['sessions'][2]['local']['address']
    end

    expect(described_class.scenario_valid?(changed, 'tunnel_vps', task_name: described_class::ADDRESSES)).to be(false)
  end

  it 'rejects a multihop TTL on a directly connected session' do
    changed = change_scenario('etheric_native') { |scenario| scenario['sessions'][0]['multihop_ttl'] = 2 }

    expect(described_class.scenario_valid?(changed, 'etheric_native', task_name: described_class::ADDRESSES)).to be(false)
  end

  it 'rejects a multihop session with a TTL below its hop count' do
    changed = change_scenario('tunnel_upstream') { |scenario| scenario['sessions'][0]['multihop_ttl'] = 1 }

    expect(described_class.scenario_valid?(changed, 'tunnel_upstream', task_name: described_class::ADDRESSES)).to be(false)
  end

  it 'rejects an IPv6 default route on the management interfaces' do
    changed = described_class.changed { |copy| copy['testbed_routing_management']['ipv6_default_route'] = true }

    expect(described_class.valid?(changed, described_class::MANAGEMENT)).to be(false)
  end

  it 'rejects two tunnels with the same endpoint tuple' do
    changed = change_scenario('tunnel_vps') do |scenario, copy|
      static_vps = copy['service_mapping']['routing_tunnel_static_vps_suburban']
      scenario['tunnels'][0]['remote']['outer_ipv4'] = static_vps['routing_interfaces']['outer']['ipv4']
    end

    expect(described_class.identity_valid?(changed, 'tunnel_endpoint_tuple')).to be(false)
  end

  it 'rejects two guests with the same VMID' do
    changed = described_class.changed do |copy|
      mapping = copy['service_mapping']
      mapping['routing_tunnel_vps_client_suburban']['vmid'] = mapping['routing_tunnel_vps_upstream_suburban']['vmid']
    end

    expect(described_class.identity_valid?(changed, 'vmid')).to be(false)
  end

  it 'rejects an upstream router that two scenarios reference' do
    changed = change_scenario('tunnel_vps') do |scenario|
      scenario['nodes'] = %w[tunnel_vps_vps tunnel_static_upstream tunnel_vps_client]
    end

    expect(described_class.scenario_valid?(changed, 'tunnel_vps')).to be(false)
  end

  it 'renders the Astound reservation, gateway address, and connection from one address' do
    mapping = described_class.changed { |copy| copy['service_mapping']['mwan_suburban']['ipv4_astound'] = '10.240.207.9' }

    expect(described_class.astound_addresses(mapping.fetch('service_mapping'))).to eq(
      reservation: '10.240.207.9', gateway: '10.240.207.9/24', connection: '10.240.207.9'
    )
  end
end
