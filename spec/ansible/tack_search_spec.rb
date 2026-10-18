# frozen_string_literal: true

require 'yaml'
require_relative '../support/tack_search_inventory'

# These examples check the search guest identities in the service mapping,
# the application search settings a deploy renders, and the QA capacity
# values.
module TackSearchEnvironment
  GROUP_VARS_DIRECTORY = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'inventory', 'group_vars')
  PRODUCTION_ENDPOINT = 'https://[3d06:bad:b01::254]:9200'
  QA_ENDPOINT = 'https://[3d06:bad:b01:210::5]:9200'
  # The spec requires the env file to render every setting in SETTING_NAMES.
  # Most settings lack compiled defaults in Tack. The deployment plan requires
  # OPENSEARCH_PUBLIC_ENABLED. Configs renders OPENSEARCH_MODEL_REPAIR_ENABLED
  # explicitly for each environment despite Tack's compiled default of false.
  # PUBLIC_SETTING_NAMES adds OPENSEARCH_CURSOR_KEY_FILE. Tack reads the key only
  # while public search is on. The env file renders OPENSEARCH_CURSOR_KEY_FILE only
  # while public search is on.
  SETTING_NAMES = %w[
    OPENSEARCH_CA OPENSEARCH_ENDPOINT OPENSEARCH_MODEL_REPAIR_ENABLED OPENSEARCH_PASSWORD_FILE
    OPENSEARCH_PUBLIC_ENABLED OPENSEARCH_REPLICAS OPENSEARCH_ROUTING_SHARDS OPENSEARCH_SHARDS
    OPENSEARCH_USERNAME OPENSEARCH_MAINTENANCE_USERNAME OPENSEARCH_MAINTENANCE_PASSWORD_FILE
  ].freeze
  PUBLIC_SETTING_NAMES = (SETTING_NAMES + %w[OPENSEARCH_CURSOR_KEY_FILE]).sort.freeze
  PUBLIC = { 'tack_search_public_enabled' => true }.freeze

  module_function

  def group_vars(name)
    YAML.safe_load_file(File.join(GROUP_VARS_DIRECTORY, name), aliases: true)
  end

  def service_mapping
    group_vars(File.join('all', 'service_mapping.yml')).fetch('service_mapping')
  end

  def duplicates(values)
    values.tally.select { |_, count| count > 1 }.keys
  end

  def search_settings(rendered)
    rendered.settings(rendered.owner).select { |name, _| name.start_with?('OPENSEARCH_') }
  end
end

RSpec.describe TackSearchEnvironment do
  let(:service_mapping) { described_class.service_mapping }

  it 'reserves distinct production and QA search guests without changing data guests' do
    expect(service_mapping.fetch('tack_search1')).to include(
      'vmid' => 125, 'ipv6' => '3d06:bad:b01::125', 'mac_address' => 'BC:24:11:A3:52:24',
      'docker_v6_subnet' => '3d06:bad:b01:0:7b1::/96'
    )
    expect(service_mapping.fetch('tack_search1_suburban')).to include(
      'vmid' => 227, 'ipv6' => '3d06:bad:b01:210::227', 'mac_address' => 'BC:24:11:04:02:27',
      'docker_v6_subnet' => '3d06:bad:b01:210:7b1::/96'
    )
    expect(service_mapping.keys.grep(/\Atack_search/)).to contain_exactly('tack_search1', 'tack_search1_suburban')
    expect(%w[tack_data1 tack_data2 tack_data3].map { |name| service_mapping.fetch(name).fetch('vmid') }).to eq([120, 121, 122])
  end

  it 'assigns every guest a distinct VMID, MAC address, address, and Docker subnet', :aggregate_failures do
    guests = service_mapping.values.select { |entry| entry.key?('vmid') }

    expect(described_class.duplicates(guests.map { |entry| entry['vmid'] })).to be_empty
    expect(described_class.duplicates(guests.filter_map { |entry| entry['mac_address']&.upcase })).to be_empty
    expect(described_class.duplicates(guests.filter_map { |entry| entry['ipv6'] })).to be_empty
    expect(described_class.duplicates(guests.filter_map { |entry| entry['docker_v6_subnet'] })).to be_empty
  end

  it 'keeps one application endpoint while the production member count grows' do
    one = TackSearchInventory.rendered(:production, member_count: 1)
    three = TackSearchInventory.rendered(:production, member_count: 3)

    endpoint = one.settings(one.owner).fetch('OPENSEARCH_ENDPOINT')

    expect(endpoint).to eq(TackSearchEnvironment::PRODUCTION_ENDPOINT)
    expect(three.settings(three.owner).fetch('OPENSEARCH_ENDPOINT')).to eq(endpoint)
  end

  it 'the QA configuration enables public search with zero replicas' do
    rendered = TackSearchInventory.rendered(:qa)

    expect(described_class.search_settings(rendered)).to eq(
      'OPENSEARCH_ENDPOINT' => TackSearchEnvironment::QA_ENDPOINT,
      'OPENSEARCH_CA' => '/etc/tack/search-ca.crt',
      'OPENSEARCH_USERNAME' => 'tack_runtime',
      'OPENSEARCH_PASSWORD_FILE' => '/run/secrets/OPENSEARCH_PASSWORD',
      'OPENSEARCH_MAINTENANCE_USERNAME' => 'tack_maintenance',
      'OPENSEARCH_MAINTENANCE_PASSWORD_FILE' => '/run/secrets/OPENSEARCH_MAINTENANCE_PASSWORD',
      'OPENSEARCH_SHARDS' => '1',
      'OPENSEARCH_ROUTING_SHARDS' => '8',
      'OPENSEARCH_REPLICAS' => '0',
      'OPENSEARCH_PUBLIC_ENABLED' => 'true',
      'OPENSEARCH_MODEL_REPAIR_ENABLED' => 'true',
      'OPENSEARCH_CURSOR_KEY_FILE' => '/run/secrets/OPENSEARCH_CURSOR_KEY'
    )
    expect(described_class.search_settings(rendered).keys.sort).to eq(TackSearchEnvironment::PUBLIC_SETTING_NAMES)
  end

  it 'the production configuration enables public search, keeps model repair off, and ends the bootstrap', :aggregate_failures do
    rendered = TackSearchInventory.rendered(:production)

    expect(described_class.search_settings(rendered)).to eq(
      'OPENSEARCH_ENDPOINT' => TackSearchEnvironment::PRODUCTION_ENDPOINT,
      'OPENSEARCH_CA' => '/etc/tack/search-ca.crt',
      'OPENSEARCH_USERNAME' => 'tack_runtime',
      'OPENSEARCH_PASSWORD_FILE' => '/run/secrets/OPENSEARCH_PASSWORD',
      'OPENSEARCH_MAINTENANCE_USERNAME' => 'tack_maintenance',
      'OPENSEARCH_MAINTENANCE_PASSWORD_FILE' => '/run/secrets/OPENSEARCH_MAINTENANCE_PASSWORD',
      'OPENSEARCH_SHARDS' => '1',
      'OPENSEARCH_ROUTING_SHARDS' => '8',
      'OPENSEARCH_REPLICAS' => '0',
      'OPENSEARCH_PUBLIC_ENABLED' => 'true',
      'OPENSEARCH_MODEL_REPAIR_ENABLED' => 'false',
      'OPENSEARCH_CURSOR_KEY_FILE' => '/run/secrets/OPENSEARCH_CURSOR_KEY'
    )
    expect(rendered.settings(rendered.member_host('tack_search1'))).to include(
      'TACK_SEARCH_INITIAL_CLUSTER_MANAGER_NODES' => ''
    )
  end

  { production: 'false', qa: 'true' }.each do |environment, model_repair|
    it "renders the #{environment} cursor key file when public search is on" do
      rendered = TackSearchInventory.rendered(environment, overrides: TackSearchEnvironment::PUBLIC)
      settings = described_class.search_settings(rendered)

      expect(settings).to include(
        'OPENSEARCH_PUBLIC_ENABLED' => 'true',
        'OPENSEARCH_MODEL_REPAIR_ENABLED' => model_repair,
        'OPENSEARCH_CURSOR_KEY_FILE' => '/run/secrets/OPENSEARCH_CURSOR_KEY'
      )
      expect(settings.keys.sort).to eq(TackSearchEnvironment::PUBLIC_SETTING_NAMES)
    end
  end

  it 'keeps QA at one single-node search guest within the suburban capacity', :aggregate_failures do
    cluster = described_class.group_vars(File.join('all', 'search_cluster.yml'))
    rendered = TackSearchInventory.rendered(:qa)
    settings = rendered.settings(rendered.member_host('tack_search1_suburban'))

    expect(described_class.group_vars('suburban_servers.yml').fetch('tack_search_qa_host_min_available_gib')).to eq(6.26)
    expect(cluster.fetch('tack_search_guest_memory_mib')).to eq(8192)
    expect(cluster.fetch('tack_search_guest_disk_gib')).to eq(40)
    expect(cluster.fetch('tack_search_guest_cores')).to eq(2)
    expect(cluster.fetch('tack_search_qa_members')).to eq(['tack_search1_suburban'])
    expect(settings).to include(
      'TACK_SEARCH_DISCOVERY_TYPE' => 'single-node', 'TACK_SEARCH_SEED_HOSTS' => '',
      'TACK_SEARCH_INITIAL_CLUSTER_MANAGER_NODES' => ''
    )
  end
end
