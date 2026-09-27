# frozen_string_literal: true

require 'yaml'
require_relative '../support/tack_search_inventory'

# The search guests' identities, the application's view of the search
# endpoint, and the QA capacity gate, read from the files a deploy renders.
module TackSearchEnvironment
  GROUP_VARS_DIRECTORY = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'inventory', 'group_vars')
  PRODUCTION_ENDPOINT = 'https://[3d06:bad:b01::254]:9200'
  QA_ENDPOINT = 'https://[3d06:bad:b01:210::5]:9200'
  # Every variable Tack's config package reads for search that has no
  # compiled default, plus the public switch the deployment plan requires.
  SETTING_NAMES = %w[
    OPENSEARCH_CA OPENSEARCH_ENDPOINT OPENSEARCH_PASSWORD OPENSEARCH_PUBLIC_ENABLED
    OPENSEARCH_REPLICAS OPENSEARCH_ROUTING_SHARDS OPENSEARCH_SHARDS OPENSEARCH_USERNAME
  ].freeze

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

  it 'keeps one application endpoint while production members grow' do
    one = TackSearchInventory.rendered(:production, member_count: 1)
    three = TackSearchInventory.rendered(:production, member_count: 3)

    endpoint = one.settings(one.owner).fetch('OPENSEARCH_ENDPOINT')

    expect(endpoint).to eq(TackSearchEnvironment::PRODUCTION_ENDPOINT)
    expect(three.settings(three.owner).fetch('OPENSEARCH_ENDPOINT')).to eq(endpoint)
  end

  {
    production: TackSearchEnvironment::PRODUCTION_ENDPOINT,
    qa: TackSearchEnvironment::QA_ENDPOINT
  }.each do |environment, endpoint|
    it "renders the #{environment} search settings Tack reads, with public search off and no replica" do
      rendered = TackSearchInventory.rendered(environment)
      prefix = environment == :qa ? 'vault_tack_qa' : 'vault_tack'

      expect(described_class.search_settings(rendered)).to eq(
        'OPENSEARCH_ENDPOINT' => endpoint,
        'OPENSEARCH_CA' => '/etc/tack/search-ca.crt',
        'OPENSEARCH_USERNAME' => "render-only-#{prefix}_search_username",
        'OPENSEARCH_PASSWORD' => "render-only-#{prefix}_search_password",
        'OPENSEARCH_SHARDS' => '1',
        'OPENSEARCH_ROUTING_SHARDS' => '8',
        'OPENSEARCH_REPLICAS' => '0',
        'OPENSEARCH_PUBLIC_ENABLED' => 'false'
      )
      expect(described_class.search_settings(rendered).keys.sort).to eq(TackSearchEnvironment::SETTING_NAMES)
    end
  end

  it 'keeps QA inside the suburban capacity gate', :aggregate_failures do
    cluster = described_class.group_vars(File.join('all', 'search_cluster.yml'))
    rendered = TackSearchInventory.rendered(:qa)
    environment = rendered.yaml(rendered.member_host('tack_search1_suburban'), 'override')
                          .dig('services', 'opensearch', 'environment')

    expect(described_class.group_vars('suburban_servers.yml').fetch('tack_search_qa_host_min_available_gib')).to eq(6.26)
    expect(cluster.fetch('tack_search_guest_memory_mib')).to eq(8192)
    expect(cluster.fetch('tack_search_guest_disk_gib')).to eq(40)
    expect(cluster.fetch('tack_search_guest_cores')).to eq(2)
    expect(cluster.fetch('tack_search_qa_members')).to eq(['tack_search1_suburban'])
    expect(environment).to include('discovery.type' => 'single-node')
    expect(environment.keys).not_to include('discovery.seed_hosts', 'cluster.initial_cluster_manager_nodes')
  end
end
