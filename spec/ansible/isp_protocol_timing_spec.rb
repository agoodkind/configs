# frozen_string_literal: true

require 'json'
require 'yaml'
require_relative '../support/task_expressions'

RSpec.describe 'ISP simulator protocol timing' do
  let(:root) { AnsibleRender::REPOSITORY_ROOT }
  let(:inventory) do
    YAML.safe_load_file(File.join(root, 'ansible/inventory/group_vars/suburban_servers.yml'))
  end
  let(:include_task) do
    plays = YAML.safe_load_file(File.join(root, 'ansible/playbooks/deploy-testbed.yml'))
    plays.fetch(1).fetch('tasks').find do |task|
      task['name'] == 'Render and push ISP LXC configs'
    end
  end
  let(:provider) do
    providers = inventory.fetch('testbed_isp_lxcs')
    providers.find { |entry| entry.fetch('name') == 'mbrains' }
  end

  def render_protocols(isp_entry)
    templates = {
      'dhcp4' => File.read(File.join(root, 'testbed/isp-lxc/kea-dhcp4.conf.j2')),
      'dhcp6' => File.read(File.join(root, 'testbed/isp-lxc/kea-dhcp6.conf.j2')),
      'ra' => File.read(File.join(root, 'testbed/isp-lxc/radvd.conf.j2'))
    }
    result = TaskExpressions.evaluate(
      variables: {
        'isp_entry' => isp_entry,
        'testbed_isp_protocol_defaults' => inventory.fetch('testbed_isp_protocol_defaults')
      },
      facts: [], renders: [TaskExpressions.render_task(include_task, templates)]
    )
    result.fetch('renders').first
  end

  def valid_protocol_timing?(isp_entry)
    tasks = YAML.safe_load_file(File.join(root, 'ansible/playbooks/tasks/deploy-testbed-isp-lxc.yml'))
    assertions = tasks.first.fetch('ansible.builtin.assert').fetch('that')
    provider_values = inventory.fetch('testbed_isp_protocol_defaults').merge(isp_entry)
    result = TaskExpressions.evaluate(
      variables: { 'isp' => provider_values }, facts: [],
      conditions: { 'protocol timing' => assertions }
    )
    result.fetch('conditions').fetch('protocol timing')
  end

  it 'preserves the current provider timing when no override is set' do
    rendered = render_protocols(provider)
    dhcp4 = JSON.parse(rendered.fetch('dhcp4')).fetch('Dhcp4')
    dhcp6 = JSON.parse(rendered.fetch('dhcp6')).fetch('Dhcp6')

    expect(dhcp4.fetch('valid-lifetime')).to eq(4000)
    expect(dhcp4).not_to have_key('renew-timer')
    expect(dhcp4).not_to have_key('rebind-timer')
    expect(dhcp6.fetch('preferred-lifetime')).to eq(3000)
    expect(dhcp6.fetch('valid-lifetime')).to eq(4000)
    expect(dhcp6).not_to have_key('renew-timer')
    expect(dhcp6).not_to have_key('rebind-timer')
    expect(rendered.fetch('ra')).to include('MinRtrAdvInterval 30;')
    expect(rendered.fetch('ra')).to include('MaxRtrAdvInterval 100;')
    expect(rendered.fetch('ra')).not_to include('AdvValidLifetime')
    expect(rendered.fetch('ra')).not_to include('AdvPreferredLifetime')
  end

  it 'renders an isolated short-lease scenario through Ansible' do
    scenario = provider.merge(
      'dhcp4_valid_lifetime_seconds' => 12,
      'dhcp4_renew_seconds' => 4,
      'dhcp4_rebind_seconds' => 8,
      'dhcp6_preferred_lifetime_seconds' => 10,
      'dhcp6_valid_lifetime_seconds' => 15,
      'dhcp6_renew_seconds' => 4,
      'dhcp6_rebind_seconds' => 8,
      'ra_min_interval_seconds' => 3,
      'ra_max_interval_seconds' => 9,
      'ra_prefix_preferred_lifetime_seconds' => 12,
      'ra_prefix_valid_lifetime_seconds' => 7200
    )
    rendered = render_protocols(scenario)
    dhcp4 = JSON.parse(rendered.fetch('dhcp4')).fetch('Dhcp4')
    dhcp6 = JSON.parse(rendered.fetch('dhcp6')).fetch('Dhcp6')

    expect(dhcp4.values_at('valid-lifetime', 'renew-timer', 'rebind-timer')).to eq([12, 4, 8])
    expect(dhcp6.values_at('preferred-lifetime', 'valid-lifetime', 'renew-timer', 'rebind-timer'))
      .to eq([10, 15, 4, 8])
    expect(rendered.fetch('ra')).to include('MinRtrAdvInterval 3;')
    expect(rendered.fetch('ra')).to include('MaxRtrAdvInterval 9;')
    expect(rendered.fetch('ra')).to include('AdvPreferredLifetime 12;')
    expect(rendered.fetch('ra')).to include('AdvValidLifetime 7200;')
  end

  it 'advertises an explicit zero preferred lifetime for prefix deprecation' do
    scenario = provider.merge(
      'ra_prefix_preferred_lifetime_seconds' => 0,
      'ra_prefix_valid_lifetime_seconds' => 7200
    )
    rendered = render_protocols(scenario)

    expect(rendered.fetch('ra')).to include('AdvPreferredLifetime 0;')
    expect(rendered.fetch('ra')).to include('AdvValidLifetime 7200;')
    expect(valid_protocol_timing?(scenario)).to be(true)
  end

  it 'rejects a valid RA lifetime without an explicit preferred lifetime' do
    scenario = provider.merge('ra_prefix_valid_lifetime_seconds' => 0)

    expect(valid_protocol_timing?(scenario)).to be(false)
  end
end
