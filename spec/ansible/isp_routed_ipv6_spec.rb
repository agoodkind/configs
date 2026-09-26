# frozen_string_literal: true

require 'yaml'
require_relative '../support/task_expressions'

RSpec.describe 'ISP simulator routing' do
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
    if provider.fetch('ipv6_enabled') || provider.fetch('routed_ipv6_enabled') ||
       !provider.fetch('routed_ipv4_prefix').empty? || !provider.fetch('static_v4_block').empty?
      templates['routes'] = File.read(File.join(root, 'testbed/isp-lxc/pd-route.service.j2'))
    end
    TaskExpressions.evaluate(
      variables: { 'isp' => provider }, facts: [], conditions: conditions,
      renders: [{ 'vars' => {}, 'templates' => templates }]
    )
  end

  it 'renders native IPv4 and IPv6 return routes without delegation or router advertisements' do
    provider = providers.find { |entry| entry.fetch('name') == 'routed' }
    result = render_simulator(provider)
    rendered = result.fetch('renders').first
    prefix = provider.fetch('routed_ipv6_prefix')
    ipv6_route = "ExecStart=/sbin/ip -6 route replace #{prefix} via #{provider.fetch('mwan_vm_ll')} dev eth0"
    ipv4_prefix = provider.fetch('routed_ipv4_prefix')
    ipv4_route = "ExecStart=/sbin/ip -4 route replace #{ipv4_prefix} via #{provider.fetch('routed_ipv4_route_to')} dev eth0"

    expect(rendered.fetch('routes').lines.grep(/^ExecStart=/).map(&:strip)).to eq([ipv6_route, ipv4_route])
    expect(rendered.fetch('firewall')).to include("oifname \"eth1\" ip6 saddr #{prefix} masquerade")
    expect(rendered.fetch('firewall')).to include("oifname \"eth1\" ip saddr #{provider.fetch('v4_subnet')} masquerade")
    expect(rendered.fetch('firewall')).to include("oifname \"eth1\" ip saddr #{ipv4_prefix} masquerade")
    %w[Render Push].each do |verb|
      name = result.fetch('conditions').keys.find { |key| key.start_with?("#{verb} ISP return routes") }
      expect(result.fetch('conditions').fetch(name)).to be(true)
    end
    expect(result.fetch('conditions').fetch('Enable and restart ISP return routes')).to be(true)
    expect(result.fetch('conditions').fetch('Render ISP DHCPv6 and RA templates to suburban /tmp')).to be(false)
    expect(result.fetch('conditions').fetch('Push ISP DHCPv6 and RA configs into LXC')).to be(false)
    expect(result.fetch('conditions').fetch('Enable and restart ISP DHCPv6 and RA services')).to be(false)
  end

  it 'selects return routes, DHCPv6 services, and firewall rules by simulator mode' do
    providers.each do |provider|
      result = render_simulator(provider)
      rendered = result.fetch('renders').first
      delegated = provider.fetch('ipv6_enabled')
      routed = provider.fetch('routed_ipv6_enabled')
      routed_ipv4 = !provider.fetch('routed_ipv4_prefix').empty?
      static_ipv4 = !provider.fetch('static_v4_block').empty?
      expect(result.fetch('conditions').fetch('Enable and restart ISP return routes')).to eq(
        delegated || routed || routed_ipv4 || static_ipv4
      )
      expect(result.fetch('conditions').fetch('Enable and restart ISP DHCPv6 and RA services')).to eq(delegated)
      expect(rendered.fetch('firewall').include?('table ip6 nat')).to eq(delegated || routed)
      next unless delegated || routed

      prefix = delegated ? provider.fetch('pd_prefix') : provider.fetch('routed_ipv6_prefix')
      expect(rendered.fetch('routes')).to include("route replace #{prefix} via #{provider.fetch('mwan_vm_ll')}")
      expect(rendered.fetch('firewall')).to include("ip6 saddr #{prefix} masquerade")
    end
  end
end
