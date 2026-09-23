# frozen_string_literal: true

require 'yaml'
require_relative '../support/task_expressions'

RSpec.describe 'ISP simulator IPv6 routing' do
  let(:root) { AnsibleRender::REPOSITORY_ROOT }
  let(:providers) do
    YAML.safe_load_file(File.join(root, 'ansible/inventory/group_vars/suburban_servers.yml')).fetch('testbed_isp_lxcs')
  end
  let(:tasks) do
    YAML.safe_load_file(File.join(root, 'ansible/playbooks/tasks/deploy-testbed-isp-lxc.yml'))
  end

  def render_simulator(provider)
    conditions = tasks.to_h do |task|
      [task.fetch('name'), TaskExpressions.condition_list(task['when'])]
    end
    templates = { 'firewall' => File.read(File.join(root, 'testbed/isp-lxc/nftables.conf.j2')) }
    if provider.fetch('ipv6_enabled') || provider.fetch('routed_ipv6_enabled')
      templates['routes'] = File.read(File.join(root, 'testbed/isp-lxc/pd-route.service.j2'))
    end
    TaskExpressions.evaluate(
      variables: { 'isp' => provider }, facts: [], conditions: conditions,
      renders: [{ 'vars' => {}, 'templates' => templates }]
    )
  end

  it 'renders a configured return route and IPv6 egress without delegation or router advertisements' do
    provider = providers.find { |entry| entry.fetch('name') == 'webpass' }.reject do |key, _|
      %w[pd_prefix pd_len ia_na slaac_prefix].include?(key)
    end.merge('ipv6_enabled' => false, 'routed_ipv6_enabled' => true, 'routed_ipv6_prefix' => '2001:db8:340::/48')
    result = render_simulator(provider)
    rendered = result.fetch('renders').first
    route = "ExecStart=/sbin/ip -6 route replace 2001:db8:340::/48 via #{provider.fetch('mwan_vm_ll')} dev eth0"

    expect(rendered.fetch('routes').lines.grep(/^ExecStart=/).map(&:strip)).to eq([route])
    expect(rendered.fetch('firewall')).to include('oifname "eth1" ip6 saddr 2001:db8:340::/48 masquerade')
    expect(rendered.fetch('firewall')).to include("oifname \"eth1\" ip saddr #{provider.fetch('v4_subnet')} masquerade")
    %w[Render Push].each do |verb|
      name = result.fetch('conditions').keys.find { |key| key.start_with?("#{verb} ISP return routes") }
      expect(result.fetch('conditions').fetch(name)).to be(true)
    end
    expect(result.fetch('conditions').fetch('Enable and restart ISP return routes')).to be(true)
    expect(result.fetch('conditions').fetch('Render ISP DHCPv6 and RA templates to suburban /tmp')).to be(false)
    expect(result.fetch('conditions').fetch('Push ISP DHCPv6 and RA configs into LXC')).to be(false)
    expect(result.fetch('conditions').fetch('Enable and restart ISP DHCPv6 and RA services')).to be(false)
  end

  it 'preserves the configured delegated and IPv4-only simulator behavior' do
    providers.each do |provider|
      result = render_simulator(provider)
      rendered = result.fetch('renders').first
      enabled = provider.fetch('ipv6_enabled')
      expect(result.fetch('conditions').fetch('Enable and restart ISP return routes')).to eq(enabled)
      expect(result.fetch('conditions').fetch('Enable and restart ISP DHCPv6 and RA services')).to eq(enabled)
      expect(rendered.fetch('firewall').include?('table ip6 nat')).to eq(enabled)
      next unless enabled

      expect(rendered.fetch('routes')).to include("route replace #{provider.fetch('pd_prefix')} via #{provider.fetch('mwan_vm_ll')}")
      expect(rendered.fetch('firewall')).to include("ip6 saddr #{provider.fetch('pd_prefix')} masquerade")
    end
  end
end
