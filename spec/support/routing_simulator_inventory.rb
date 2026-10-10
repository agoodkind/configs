# frozen_string_literal: true

require 'yaml'
require_relative 'task_expressions'

# The helper evaluates assertions from validate-routing-simulators.yml with
# TaskExpressions against the repository inventory or a changed copy.
module RoutingSimulatorInventory
  GROUP_VARS_DIRECTORY = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'inventory', 'group_vars')
  ROUTING_DIRECTORY = File.join(GROUP_VARS_DIRECTORY, 'testbed_routing_all')
  TASKS_DIRECTORY = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks', 'tasks')
  ENTRY_FILE = 'validate-routing-simulators.yml'
  ASSERT_KEY = 'ansible.builtin.assert'
  IMPORT_KEY = 'ansible.builtin.import_tasks'
  KEYS = 'Require every routing scenario key'
  NODES = 'Require supported routing simulator nodes and existing references'
  MANAGEMENT = 'Require management interfaces outside test forwarding'
  NETWORKS = 'Require declared routing simulator networks'
  ADDRESS_OWNERS = 'Require one owner for each routing simulator address'
  IDENTITIES = 'Require unique routing simulator identities'
  SCENARIOS = 'Require each routing scenario to satisfy the contract of its kind'
  ADDRESSES = 'Require each tunnel and session address to match its node and link'
  SCENARIO_TASKS = [KEYS, SCENARIOS, ADDRESSES].freeze
  SCENARIO_VARIABLE_PREFIX = 'testbed_routing_scenario_'
  # ansible-core renders FIRST_VARIABLES before each scenario variable.
  # ansible-core renders testbed_routing_scenarios after every scenario
  # variable.
  FIRST_VARIABLES = %w[
    testbed_routing_home_prefix testbed_routing_management_unreachable_prefixes testbed_routing_connections
    testbed_routing_nodes
  ].freeze
  LAST_VARIABLES = %w[testbed_routing_scenarios].freeze
  LITERAL_VARIABLES = %w[
    service_mapping testbed_routing_networks testbed_routing_guests testbed_routing_management
    routing_simulator_roles routing_simulator_route_families routing_simulator_tunnel_protocols
    routing_simulator_private_asn_ranges routing_simulator_contracts
  ].freeze

  module_function

  def group_vars(name)
    YAML.safe_load_file(File.join(GROUP_VARS_DIRECTORY, name), aliases: true)
  end

  def routing_vars
    Dir.glob(File.join(ROUTING_DIRECTORY, '*.yml')).map { |file| YAML.safe_load_file(file) }.reduce(:merge)
  end

  def inventory
    group_vars(File.join('all', 'service_mapping.yml')).slice(*LITERAL_VARIABLES).merge(routing_vars)
  end

  def templated_variables(inventory)
    scenario_variables = inventory.keys.select { |name| name.start_with?(SCENARIO_VARIABLE_PREFIX) }
    FIRST_VARIABLES + scenario_variables + LAST_VARIABLES
  end

  def tasks(directory: TASKS_DIRECTORY, entry_file: ENTRY_FILE)
    entry_path = File.join(directory, entry_file)
    task_list(entry_path).flat_map do |entry|
      raise ArgumentError, "#{entry_path}: an entry lacks #{IMPORT_KEY}" unless entry.is_a?(Hash) && entry.key?(IMPORT_KEY)

      task_list(File.join(directory, entry.fetch(IMPORT_KEY)))
    end
  end

  def task_list(path)
    loaded = YAML.safe_load_file(path)
    raise ArgumentError, "#{path}: the file is not a task list" unless loaded.is_a?(Array)

    loaded
  rescue SystemCallError, Psych::Exception => e
    raise ArgumentError, "#{path}: #{e.message}"
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
    facts = templated_variables(inventory).map { |name| fact({ name => inventory.fetch(name) }) }
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

  def valid?(inventory, task_name) = verdict(inventory, task_name).fetch(:valid)

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

  def scenario_names(inventory) = inventory.fetch('testbed_routing_scenarios').keys

  def changed
    copy = Marshal.load(Marshal.dump(inventory))
    yield copy
    copy
  end

  def changed_scenario(name)
    changed { |copy| yield copy.fetch("#{SCENARIO_VARIABLE_PREFIX}#{name}"), copy }
  end
end
