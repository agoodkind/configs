# frozen_string_literal: true

require 'yaml'
require_relative '../support/tack_search_inventory'
require_relative '../support/task_expressions'

# These examples read each OpenSearch member as a deploy renders it, and they
# evaluate the checks the deploy runs before it starts a member.
module TackSearchNode
  PLAYBOOK_FILE = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks', 'deploy-tack.yml')
  NODE_TASKS_FILE = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks', 'tasks', 'tack-search-node.yml')
  MEMORY_MAP_TASK = 'Refuse to start OpenSearch below its memory-map limit'
  TOPOLOGY_TASK = 'Refuse a search topology the members cannot place'
  # Every member renders these OpenSearch container settings that Tack requires.
  CONTRACT = {
    'node.roles' => 'cluster_manager,data,ingest,ml',
    'plugins.ml_commons.only_run_on_ml_node' => 'true',
    'plugins.ml_commons.task_dispatch_policy' => 'least_load',
    'plugins.ml_commons.model_auto_redeploy.enable' => 'true'
  }.freeze
  # Production members run a 2 GiB heap. The QA member runs 3 GiB (D9 QA trial).
  JAVA_OPTS = {
    production: '-Xms2g -Xmx2g -Djava.net.preferIPv6Addresses=true',
    qa: '-Xms3g -Xmx3g -Djava.net.preferIPv6Addresses=true'
  }.freeze
  CLUSTER_FILE = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'inventory', 'group_vars', 'all', 'search_cluster.yml')
  # The settings of /usr/share/opensearch/config/opensearch.yml in the pinned
  # opensearchproject/opensearch:3.8.0 image, read from the image on
  # 2026-10-02 (index digest sha256:fafe3fc3...6a40). A coordinating-only
  # member mounts these settings plus an empty role list in place of that file.
  IMAGE_DEFAULT_SETTINGS = { 'cluster.name' => 'docker-cluster', 'network.host' => '0.0.0.0' }.freeze
  IMAGE_CONFIG_PATH = '/usr/share/opensearch/config/opensearch.yml'
  # One production cluster with a member for each role set. The first member
  # keeps the default combined set because it forms the cluster.
  ROLE_SET_MEMBERS = { 'tack_search2' => 'ml', 'tack_search3' => 'data', 'tack_search4' => 'coordinating' }.freeze

  module_function

  def service(rendered, member)
    rendered.yaml(rendered.member_host(member), 'override').dig('services', 'opensearch')
  end

  # The OpenSearch roles of each named role set, as the inventory declares them.
  def role_sets
    YAML.safe_load_file(CLUSTER_FILE).fetch('tack_search_role_sets')
  end

  # The inventory member lists with each assigned member added to the list of
  # each of its role sets.
  def role_set_members(assignments)
    lists = YAML.safe_load_file(CLUSTER_FILE).fetch('tack_search_role_set_members')
    assignments.each do |member, role_sets|
      Array(role_sets).each { |role_set| lists.fetch(role_set).push(member) }
    end
    lists
  end

  def task_named(file, name)
    tasks = YAML.safe_load_file(file).flat_map { |entry| entry['tasks'] || [entry] }
    task = tasks.find { |candidate| candidate['name'] == name }
    raise "#{file} has no task named #{name.inspect}" if task.nil?

    task
  end

  # The assert's conditions, evaluated after the task's own vars, the order
  # Ansible renders them in.
  def passes?(file, name, variables)
    task = task_named(file, name)
    own_vars = TaskExpressions.value_map(task['vars'])
    task_vars = { 'when' => [], 'vars' => own_vars, 'set_fact' => own_vars }
    conditions = { 'check' => TaskExpressions.condition_list(task.dig('ansible.builtin.assert', 'that')) }
    TaskExpressions.evaluate(variables: variables, facts: [task_vars], conditions: conditions).dig('conditions', 'check')
  end

  # The topology check inputs for members tack_search1 to tack_search<n>, with
  # the given role set assignments.
  def topology(replicas:, members:, single_node: false, member_role_sets: {})
    {
      'tack_search_replicas' => replicas,
      'tack_search_members' => (1..members).map { |number| "tack_search#{number}" },
      'tack_search_single_node' => single_node,
      'tack_search_role_sets' => role_sets,
      'tack_search_role_set_members' => role_set_members(member_role_sets)
    }
  end
end

RSpec.describe TackSearchNode do
  it 'renders the pinned OpenSearch image and settings on every member', :aggregate_failures do
    { production: TackSearchInventory.rendered(:production, member_count: 3),
      qa: TackSearchInventory.rendered(:qa) }.each do |environment, rendered|
      rendered.members.each do |member|
        service = described_class.service(rendered, member)

        expect(service.fetch('image')).to eq('opensearchproject/opensearch:3.8.0')
        expect(service.fetch('environment')).to include(TackSearchNode::CONTRACT)
        expect(service.fetch('environment'))
          .to include('OPENSEARCH_JAVA_OPTS' => TackSearchNode::JAVA_OPTS.fetch(environment))
        expect(service.fetch('volumes')).to include('opensearch-data:/usr/share/opensearch/data')
        expect(service.fetch('ulimits')).to eq('nofile' => { 'soft' => 65_536, 'hard' => 65_536 })
        expect(service.fetch('environment')).to include('plugins.security.ssl.http.enabled' => 'true')
      end
    end
  end

  it 'seeds production from the member list and sets the initial cluster manager only on the first member', :aggregate_failures do
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

  {
    production: ['tack_search1', 'CN=tack-search-production-admin'],
    qa: ['tack_search1_suburban', 'CN=tack-search-qa-admin']
  }.each do |environment, (member, admin_dn)|
    it "accepts the #{environment} admin certificate on the HTTP layer as #{admin_dn}" do
      service = described_class.service(TackSearchInventory.rendered(environment), member)

      expect(service.fetch('environment')).to include(
        'plugins.security.authcz.admin_dn' => admin_dn,
        'plugins.security.ssl.http.pemtrustedcas_filepath' => 'tack-search/http-ca.crt',
        'plugins.security.ssl.transport.pemtrustedcas_filepath' => 'tack-search/ca.crt'
      )
      expect(service.fetch('volumes')).to include('/etc/tack/search/http-ca.crt:/usr/share/opensearch/config/tack-search/http-ca.crt:ro')
    end
  end

  it 'maps the application user to the tack_search role and no backend role' do
    rendered = TackSearchInventory.rendered(:production)
    host = rendered.member_host('tack_search1')
    user = rendered.yaml(host, 'users').fetch('render-only-vault_tack_search_username')

    expect(user).to include('hash' => 'render-only-vault_tack_search_password_hash')
    expect(user.keys).not_to include('backend_roles')
    expect(rendered.yaml(host, 'roles_mapping').fetch('tack_search')).to include(
      'users' => ['render-only-vault_tack_search_username']
    )
  end

  {
    'the default limit 65530' => ['65530', false],
    'exactly the required limit' => ['262144', true],
    'a higher hypervisor limit' => ['1048576', true]
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
    'zero replicas on one member' => [{ replicas: 0, members: 1 }, true],
    'one replica on one member' => [{ replicas: 1, members: 1 }, false],
    'one replica on two members' => [{ replicas: 1, members: 2 }, true],
    'single-node discovery with one member' => [{ replicas: 0, members: 1, single_node: true }, true],
    'single-node discovery with two members' => [{ replicas: 0, members: 2, single_node: true }, false],
    'one replica on two members when only one has the data role' =>
      [{ replicas: 1, members: 2, member_role_sets: { 'tack_search2' => 'coordinating' } }, false],
    'one replica on a combined member and a data-only member' =>
      [{ replicas: 1, members: 2, member_role_sets: { 'tack_search2' => 'data' } }, true],
    'a first member without the cluster_manager role' =>
      [{ replicas: 0, members: 2, member_role_sets: { 'tack_search1' => 'data' } }, false],
    'a member listed under two role sets' =>
      [{ replicas: 0, members: 2, member_role_sets: { 'tack_search2' => %w[data coordinating] } }, false]
  }.each do |name, (inputs, accepted)|
    it "#{accepted ? 'accepts' : 'refuses'} #{name}" do
      variables = described_class.topology(**inputs)

      expect(described_class.passes?(TackSearchNode::PLAYBOOK_FILE, TackSearchNode::TOPOLOGY_TASK, variables)).to be(accepted)
    end
  end

  describe 'role sets' do
    let(:mixed) do
      TackSearchInventory.rendered(:production, member_count: 4,
                                                overrides: { 'tack_search_role_set_members' => described_class.role_set_members(TackSearchNode::ROLE_SET_MEMBERS) })
    end
    let(:combined_only) { TackSearchInventory.rendered(:production, member_count: 4) }

    it 'renders each member with the roles of its inventory role set', :aggregate_failures do
      role_sets = described_class.role_sets
      { 'tack_search1' => 'combined' }.merge(TackSearchNode::ROLE_SET_MEMBERS).each do |member, role_set|
        expect(described_class.service(mixed, member).fetch('environment'))
          .to include('node.roles' => role_sets.fetch(role_set).join(',')), member
      end
    end

    it 'renders the combined member byte for byte as it renders with no role set assignment' do
      host = mixed.member_host('tack_search1')

      expect(mixed.file(host, 'override')).to eq(combined_only.file(host, 'override'))
    end

    it 'keeps the ML Commons settings on every member, with or without the ml role', :aggregate_failures do
      ml_settings = TackSearchNode::CONTRACT.except('node.roles')
      mixed.members.each do |member|
        expect(described_class.service(mixed, member).fetch('environment')).to include(ml_settings), member
      end
    end

    it 'mounts the image default settings plus an empty role list only on the coordinating-only member', :aggregate_failures do
      coordinating = mixed.yaml(mixed.member_host('tack_search4'), 'override')
      mounts = coordinating.dig('services', 'opensearch', 'configs')
      source = mounts.first.fetch('source')

      expect(mounts).to eq([{ 'source' => source, 'target' => TackSearchNode::IMAGE_CONFIG_PATH }])
      expect(YAML.safe_load(coordinating.dig('configs', source, 'content')))
        .to eq(TackSearchNode::IMAGE_DEFAULT_SETTINGS.merge('node.roles' => []))
      %w[tack_search1 tack_search2 tack_search3].each do |member|
        override = mixed.yaml(mixed.member_host(member), 'override')
        expect(override.dig('services', 'opensearch')).not_to have_key('configs'), member
        expect(override).not_to have_key('configs'), member
      end
    end
  end
end
