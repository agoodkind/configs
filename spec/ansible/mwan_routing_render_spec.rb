# frozen_string_literal: true

require 'json'
require 'tmpdir'
require_relative '../support/ansible_render'

RSpec.describe 'MWAN routing scenario configuration' do
  let(:root) { AnsibleRender::REPOSITORY_ROOT }
  let(:entries) do
    Dir.mktmpdir('mwan-routing-render') do |directory|
      network = File.join(directory, 'network.json')
      AnsibleRender.render(
        inventory: 'localhost,', playbook: 'render_mwan_network.yml',
        extra_vars: { 'repository_root' => root, 'network_output' => network }
      )
      interfaces = JSON.parse(File.read(network)).fetch('ietf-interfaces:interfaces').fetch('interface')
      interfaces.to_h { |entry| [entry.fetch('name'), entry] }
    end
  end
  let(:sessions) do
    entries.transform_values { |entry| entry.fetch('goodkind-mwan-steering:wan', {}).fetch('bgp-session', []) }
  end

  def render_runtime(directory, parent_group_file, group_file)
    runtime = File.join(directory, 'config.toml')
    AnsibleRender.render(
      inventory: 'localhost,', playbook: 'render_mwan_runtime.yml',
      extra_vars: {
        'repository_root' => root, 'runtime_output' => runtime,
        'network_output' => File.join(directory, 'network.json'),
        'mwan_runtime_parent_group_file' => parent_group_file, 'mwan_runtime_group_file' => group_file,
        'mwan_runtime_management_mac' => 'BC:24:11:00:00:01', 'mwan_runtime_transit_mac' => 'BC:24:11:00:00:02'
      }
    )
    File.read(runtime)
  end

  it 'renders the upstream tunnel link, inner address, and configured route' do
    upstream = entries.fetch('sit-upstream')

    expect(upstream.fetch('type')).to eq('iana-if-type:tunnel')
    expect(upstream.fetch('goodkind-mwan-steering:link')).to eq(
      'mtu' => 1480,
      'tunnel' => {
        'protocol' => '6in4', 'underlay' => 'ensonic2', 'remote-address' => '198.51.100.30',
        'local-address' => '10.240.210.2', 'ttl' => 64
      }
    )
    expect(upstream.fetch('ietf-ip:ipv6')).to eq(
      'address' => [{ 'ip' => '2001:db8:4ff:1::1', 'prefix-length' => 64 }],
      'goodkind-mwan-steering:route' => [
        { 'destination' => '2001:db8:4ff:10::/64', 'gateway' => '2001:db8:4ff:1::2' }
      ],
      'goodkind-mwan-steering:translation' => { 'mode' => 'native' }
    )
  end

  it 'renders the multihop upstream session with its timers and prefix rules' do
    expect(sessions.fetch('sit-upstream')).to eq(
      [{
        'name' => 'tunnel_upstream_home', 'peer-address' => '2001:db8:4ff:10::2',
        'local-address' => '2001:db8:4ff:1::1', 'local-as' => 64_512, 'remote-as' => 64_541,
        'router-id' => '10.240.240.3', 'multihop-ttl' => 2, 'keepalive' => 3, 'hold' => 9,
        'connect-retry' => 5, 'route-metric' => 8600,
        'import' => [
          { 'prefix' => '2001:db8:480:1::/64', 'min-length' => 64, 'max-length' => 64 },
          { 'prefix' => '::/0', 'min-length' => 0, 'max-length' => 0 }
        ],
        'export' => [
          { 'prefix' => '3d06:bad:b01:210::/60', 'mode' => 'always', 'next-hop' => '2001:db8:4ff:1::1' }
        ]
      }]
    )
  end

  it 'renders one session on each BGP connection and no multihop TTL on a shared link' do
    configured = sessions.reject { |_name, list| list.empty? }
    names = configured.transform_values { |list| list.map { |session| session.fetch('name') } }

    expect(names).to eq(
      'enetheric0' => ['etheric_native_home'], 'sit-upstream' => ['tunnel_upstream_home'],
      'sit-vps-sonic' => ['tunnel_vps_home_sonic_1'], 'sit-vps-astound' => ['tunnel_vps_home_astound']
    )
    %w[enetheric0 sit-vps-sonic sit-vps-astound].each do |name|
      expect(sessions.fetch(name).first).not_to have_key('multihop-ttl')
    end
  end

  it 'renders a default gateway on each tunnel without a session and each tunnel on its connection' do
    { 'sit-astound' => '2001:db8:1ff:1::2', 'sit-static' => '2001:db8:3ff:1::2' }.each do |name, gateway|
      expect(entries.fetch(name).dig('ietf-ip:ipv6', 'goodkind-mwan-steering:gateway')).to eq(gateway)
    end
    underlays = entries.transform_values { |entry| entry.dig('goodkind-mwan-steering:link', 'tunnel', 'underlay') }
    expect(underlays.compact).to eq(
      'sit-astound' => 'enastound0', 'sit-static' => 'ensonic1', 'sit-upstream' => 'ensonic2',
      'sit-vps-sonic' => 'ensonic1', 'sit-vps-astound' => 'enastound0'
    )
  end

  it 'renders a distinct forced DSCP value on each scenario connection and leaves AT&T unchanged' do
    forced = entries.transform_values { |entry| entry.dig('goodkind-mwan-steering:wan', 'forced-dscp') }

    expect(forced.compact).to eq(
      'enatt0' => 8, 'ensonic1' => 35, 'ensonic2' => 39, 'enetheric0' => 43, 'sit-astound' => 47,
      'sit-static' => 51, 'sit-upstream' => 55, 'sit-vps-sonic' => 59, 'sit-vps-astound' => 63
    )
  end

  it 'renders the BGP session journal path for the testbed gateway' do
    Dir.mktmpdir('mwan-runtime-render') do |directory|
      rendered = render_runtime(directory, 'mwan_testbed_all.yml', 'mwan_suburban_servers.yml')

      expect(rendered).to include(
        "[ifmgr.modules.bgp_sessions]\nstate_file = \"/var/lib/mwan/owned-bgp-sessions.json\"\n"
      )
    end
  end

  it 'renders no BGP session journal table for the production gateway' do
    Dir.mktmpdir('mwan-runtime-render') do |directory|
      rendered = render_runtime(directory, 'mwan_prod_all.yml', 'mwan_servers.yml')

      expect(rendered).not_to include('[ifmgr.modules.bgp_sessions]')
    end
  end
end
