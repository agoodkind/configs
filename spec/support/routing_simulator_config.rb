# frozen_string_literal: true

require_relative 'routing_simulator_inventory'

# The helper renders the routing simulator templates with ansible-core. The
# helper evaluates the deploy and check task expressions with ansible-core.
module RoutingSimulatorConfig
  PLAYBOOK_DIRECTORY = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks')
  TEMPLATE_DIRECTORY = File.join(AnsibleRender::REPOSITORY_ROOT, 'testbed', 'routing-simulators')
  ASSERT_KEY = RoutingSimulatorInventory::ASSERT_KEY
  LITERAL_VARIABLES = (RoutingSimulatorInventory::LITERAL_VARIABLES + %w[
    testbed_routing_checks testbed_routing_home_route_tracker testbed_routing_unreachable_metric
    testbed_routing_unreachable_rule_priority
  ]).freeze

  module_function

  def inventory
    RoutingSimulatorInventory.inventory
  end

  def tasks(*path)
    YAML.safe_load_file(File.join(PLAYBOOK_DIRECTORY, *path), aliases: true)
  end

  def task(name, *path)
    tasks(*path).find { |candidate| candidate['name'] == name }
  end

  def fact(values, task_vars = {})
    RoutingSimulatorInventory.fact(values, task_vars)
  end

  def inventory_facts(inventory)
    RoutingSimulatorInventory.templated_variables(inventory).map { |name| fact({ name => inventory.fetch(name) }) }
  end

  def node_facts(inventory)
    node_tasks = tasks('tasks', 'routing-simulator-node-facts.yml')
    inventory_facts(inventory) + node_tasks.map { |candidate| TaskExpressions.fact_task(candidate) }
  end

  def evaluate_node(inventory, node, facts: [], conditions: {}, templates: {})
    sources = templates.transform_values { |file| File.read(File.join(TEMPLATE_DIRECTORY, file)) }
    TaskExpressions.evaluate(
      variables: inventory.slice(*LITERAL_VARIABLES).merge('rsim_name' => node),
      facts: node_facts(inventory) + facts, conditions: conditions,
      renders: [{ 'vars' => {}, 'templates' => sources }]
    )
  end

  # items maps a loop variable to the expression that selects its value from
  # the node configuration.
  def render(inventory, node, template, items = {})
    facts = items.map { |name, expression| fact({ name => "{{ #{expression} }}" }) }
    evaluate_node(inventory, node, facts: facts, templates: { 'rendered' => template })
      .fetch('renders').first.fetch('rendered')
  end

  def condition(inventory, node, expressions, facts: [])
    evaluate_node(inventory, node, facts: facts, conditions: { 'value' => TaskExpressions.condition_list(expressions) })
      .fetch('conditions').fetch('value')
  end

  def interface_address(inventory, service, interface, family)
    inventory.fetch('service_mapping').fetch(service).fetch('routing_interfaces').fetch(interface).fetch(family)
  end

  def network(inventory, name, family)
    inventory.fetch('testbed_routing_networks').fetch(name).fetch("#{family}_net")
  end

  def assertion_valid?(inventory, assertion, variables, facts)
    task_vars = TaskExpressions.value_map(assertion['vars'])
    TaskExpressions.evaluate(
      variables: inventory.slice(*LITERAL_VARIABLES).merge(variables),
      facts: inventory_facts(inventory) + facts + [fact(task_vars, task_vars)],
      conditions: { 'valid' => TaskExpressions.condition_list(assertion.fetch(ASSERT_KEY).fetch('that')) }
    ).fetch('conditions').fetch('valid')
  end

  def required_checks_valid?(inventory, scenario, mode, results)
    inputs = task('Collect the scenario check inputs', 'tasks', 'check-routing-scenario-inputs.yml')
    assertion = task('Require every required check', 'tasks', 'check-routing-scenario.yml')
    assertion_valid?(
      inventory, assertion, { 'routing_scenario_name' => scenario, 'routing_scenario_mode' => mode },
      [TaskExpressions.fact_task(inputs), fact({ 'routing_check_results' => results })]
    )
  end

  def captures(inventory, scenario, transport_addresses, node_vmids)
    inputs = task('Collect the scenario check inputs', 'tasks', 'check-routing-scenario-inputs.yml')
    collect = task('Collect the packet captures', 'tasks', 'check-routing-mwan-captures.yml')
    TaskExpressions.evaluate(
      variables: inventory.slice(*LITERAL_VARIABLES).merge(
        'routing_scenario_name' => scenario, 'routing_scenario_mode' => 'mwan',
        'routing_transport_addresses' => transport_addresses, 'routing_node_vmids' => node_vmids
      ),
      facts: inventory_facts(inventory) + [TaskExpressions.fact_task(inputs), TaskExpressions.fact_task(collect)]
    ).fetch('facts').fetch('routing_captures')
  end

  def request_valid?(inventory, scenario, mode)
    assertion = tasks('check-routing-simulators.yml').first.fetch('tasks').first
    variables = { 'routing_scenario' => scenario, 'routing_check_mode' => mode, 'mwan_environment' => 'testbed' }
    assertion_valid?(inventory, assertion, variables, [])
  end
end
