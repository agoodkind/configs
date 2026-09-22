# frozen_string_literal: true

require 'yaml'

REPOSITORY_ROOT = File.expand_path('../..', __dir__)
SERVICE_MAPPING_FILE = File.join(REPOSITORY_ROOT, 'ansible', 'inventory', 'group_vars', 'all', 'service_mapping.yml')
SEARCH_ENV_TEMPLATE = File.join(REPOSITORY_ROOT, 'tack', 'tack.env.j2')

RSpec.describe 'Tack OpenSearch inventory and environment' do
  let(:service_mapping) do
    YAML.safe_load_file(SERVICE_MAPPING_FILE, aliases: true).fetch('service_mapping')
  end

  it 'reserves distinct production and QA search guests without changing data guests' do
    production = service_mapping.fetch('tack_search1')
    qa = service_mapping.fetch('tack_search1_suburban')

    expect(production).to include(
      'vmid' => 125,
      'ipv6' => '3d06:bad:b01::125',
      'mac_address' => 'BC:24:11:A3:52:24',
      'docker_v6_subnet' => '3d06:bad:b01:0:7b1::/96'
    )
    expect(qa).to include(
      'vmid' => 225,
      'ipv6' => '3d06:bad:b01:210::225',
      'mac_address' => 'BC:24:11:04:02:25',
      'docker_v6_subnet' => '3d06:bad:b01:210:7b1::/96'
    )
    expect(service_mapping.fetch('tack_data1').fetch('vmid')).to eq(120)
    expect(service_mapping.fetch('tack_data2').fetch('vmid')).to eq(121)
    expect(service_mapping.fetch('tack_data3').fetch('vmid')).to eq(122)
  end

  it 'renders one stable endpoint and disables public search initially' do
    template = File.read(SEARCH_ENV_TEMPLATE)

    expect(template).to include('OPENSEARCH_ENDPOINT={{ tack_search_endpoint }}')
    expect(template).to include('OPENSEARCH_URLS={{ tack_search_urls | join(\',\') }}')
    expect(template).to include('OPENSEARCH_PUBLIC_ENABLED={{ tack_search_public_enabled')
  end

  it 'keeps the QA capacity and memory-map requirements in configuration' do
    search_vars = File.read(File.join(REPOSITORY_ROOT, 'ansible', 'inventory', 'group_vars', 'tack_search.yml'))
    expect(search_vars).to include('tack_search_memory_map_count: 262144')
    expect(search_vars).to include('tack_search_memory: 8192')
    expect(search_vars).to include('tack_search_cores: 2')
    expect(search_vars).to include('tack_search_disk_size: 40')
  end
end
