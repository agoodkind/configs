# frozen_string_literal: true

require 'yaml'
require_relative '../support/tack_search_inventory'
require_relative '../support/task_expressions'

# Each OpenSearch member as a deploy renders it, and the checks the deploy runs
# before it starts one.
module TackSearchNode
  PLAYBOOK_FILE = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks', 'deploy-tack.yml')
  NODE_TASKS_FILE = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks', 'tasks', 'tack-search-node.yml')
  MEMORY_MAP_TASK = 'Refuse to start OpenSearch below its memory-map limit'
  TOPOLOGY_TASK = 'Refuse a search topology the members cannot place'
  # The Tack container contract every member renders.
  CONTRACT = {
    'OPENSEARCH_JAVA_OPTS' => '-Xms2g -Xmx2g',
    'node.roles' => 'cluster_manager,data,ingest,ml',
    'plugins.ml_commons.only_run_on_ml_node' => 'true',
    'plugins.ml_commons.task_dispatch_policy' => 'least_load',
    'plugins.ml_commons.model_auto_redeploy.enable' => 'true'
  }.freeze

  module_function

  def service(rendered, member)
    rendered.yaml(rendered.member_host(member), 'override').dig('services', 'opensearch')
  end

  def assert_conditions(file, name)
    tasks = YAML.safe_load_file(file).flat_map { |entry| entry['tasks'] || [entry] }
    task = tasks.find { |candidate| candidate['name'] == name }
    raise "#{file} has no task named #{name.inspect}" if task.nil?

    TaskExpressions.condition_list(task.dig('ansible.builtin.assert', 'that'))
  end

  def passes?(file, name, variables)
    TaskExpressions.evaluate(variables: variables, facts: [], conditions: { 'check' => assert_conditions(file, name) })
                   .dig('conditions', 'check')
  end
end

RSpec.describe TackSearchNode do
  it 'renders the pinned role-capable container on every member', :aggregate_failures do
    [TackSearchInventory.rendered(:production, member_count: 3), TackSearchInventory.rendered(:qa)].each do |rendered|
      rendered.members.each do |member|
        service = described_class.service(rendered, member)

        expect(service.fetch('image')).to eq('opensearchproject/opensearch:3.8.0')
        expect(service.fetch('environment')).to include(TackSearchNode::CONTRACT)
        expect(service.fetch('volumes')).to include('opensearch-data:/usr/share/opensearch/data')
        expect(service.fetch('ulimits')).to eq('nofile' => { 'soft' => 65_536, 'hard' => 65_536 })
        expect(service.fetch('environment')).to include('plugins.security.ssl.http.enabled' => 'true')
      end
    end
  end

  it 'forms production from the member list and bootstraps only on the first member', :aggregate_failures do
    rendered = TackSearchInventory.rendered(:production, member_count: 3)
    seeds = '3d06:bad:b01::125,3d06:bad:b01::126,3d06:bad:b01::127'

    first, *later = rendered.members.map { |member| rendered.settings(rendered.member_host(member)) }

    expect(first).to include(
      'TACK_SEARCH_SEED_HOSTS' => seeds, 'TACK_SEARCH_INITIAL_CLUSTER_MANAGER_NODES' => 'tack-search1',
      'TACK_SEARCH_DISCOVERY_TYPE' => ''
    )
    later.each do |settings|
      expect(settings).to include(
        'TACK_SEARCH_SEED_HOSTS' => seeds, 'TACK_SEARCH_INITIAL_CLUSTER_MANAGER_NODES' => '',
        'TACK_SEARCH_DISCOVERY_TYPE' => ''
      )
    end
  end

  it 'announces each member at its own pinned address on host networking' do
    rendered = TackSearchInventory.rendered(:production)
    host = rendered.member_host('tack_search1')
    service = described_class.service(rendered, 'tack_search1')

    expect(service.fetch('network_mode')).to eq('host')
    expect(service.fetch('environment')).to include('network.host' => '3d06:bad:b01::125')
    expect(rendered.settings(host)).to include('TACK_SEARCH_NODE_NAME' => 'tack-search1')
  end

  it 'grants the application user the admin backend role' do
    rendered = TackSearchInventory.rendered(:production)
    users = rendered.yaml(rendered.member_host('tack_search1'), 'users')

    expect(users.fetch('render-only-vault_tack_search_username')).to include(
      'hash' => 'render-only-vault_tack_search_password_hash', 'backend_roles' => ['admin']
    )
  end

  {
    'the default 65530' => ['65530', false],
    'exactly the prerequisite' => ['262144', true],
    'a higher host value' => ['1048576', true]
  }.each do |name, (host_value, starts)|
    it "#{starts ? 'starts' : 'refuses to start'} OpenSearch with #{name}" do
      variables = {
        'tack_search_memory_map_count' => 262_144,
        'tack_search_memory_map_count_read' => { 'content' => ["#{host_value}\n"].pack('m0'), 'encoding' => 'base64' }
      }

      expect(described_class.passes?(TackSearchNode::NODE_TASKS_FILE, TackSearchNode::MEMORY_MAP_TASK, variables)).to be(starts)
    end
  end

  {
    'zero replicas on one member' => [0, 1, false, true],
    'one replica on one member' => [1, 1, false, false],
    'one replica on two members' => [1, 2, false, true],
    'single-node discovery with one member' => [0, 1, true, true],
    'single-node discovery with two members' => [0, 2, true, false]
  }.each do |name, (replicas, member_count, single_node, accepted)|
    it "#{accepted ? 'accepts' : 'refuses'} #{name}" do
      variables = {
        'tack_search_replicas' => replicas,
        'tack_search_members' => (1..member_count).map { |number| "tack_search#{number}" },
        'tack_search_single_node' => single_node
      }

      expect(described_class.passes?(TackSearchNode::PLAYBOOK_FILE, TackSearchNode::TOPOLOGY_TASK, variables)).to be(accepted)
    end
  end
end
