# frozen_string_literal: true

require 'yaml'
require_relative '../support/task_expressions'

RSpec.describe 'MWAN handover deployment manifests' do
  let(:root) { AnsibleRender::REPOSITORY_ROOT }
  let(:tasks) { YAML.safe_load_file(File.join(root, 'ansible/playbooks/tasks/prepare-mwan-connection-transfer.yml')) }

  def task(name)
    tasks.flat_map { |entry| [entry, *entry.fetch('block', [])] }.find { |entry| entry.fetch('name') == name }
  end

  def settings(transfer: true)
    groups = File.join(root, 'ansible/inventory/group_vars')
    variables = YAML.safe_load_file(File.join(groups, 'all/vars.yml'), aliases: true)
    variables.merge!(YAML.safe_load_file(File.join(groups, 'all/service_mapping.yml'), aliases: true))
    variables.merge!(YAML.safe_load_file(File.join(groups, 'mwan_suburban_servers.yml')))
    return variables unless transfer

    variables.merge!('mwan_role_job_timeout_seconds' => 120, 'mwan_transfer_active' => true,
                     'mwan_transfer_provider' => true, 'mwan_transfer_connection_id' => 'webpass',
                     'mwan_transfer_previous' => { 'goodkind-mwan-steering:owner' => 'networkd' },
                     'mwan_transfer_replacement' => { 'goodkind-mwan-steering:owner' => 'mwan' },
                     'mwan_transfer_packet_edge_count' => 0)
    names = ['Calculate bounded transfer jobs and readiness reads', 'Calculate the complete selected connection transfer budget']
    facts = names.map do |name|
      entry = TaskExpressions.fact_task(task(name))
      entry['vars'] = variables.merge(entry.fetch('vars'))
      entry
    end
    result = TaskExpressions.evaluate(variables: {}, facts: facts)
    variables.merge(result.fetch('facts'))
  end

  def interruptions(variables)
    selection = TaskExpressions.fact_task(task('Select only explicitly associated required inbound checks'))
    TaskExpressions.evaluate(
      variables: {}, facts: [],
      renders: [TaskExpressions.render_task({ 'vars' => variables.merge(selection.fetch('vars')) }, selection.fetch('set_fact'))]
    ).fetch('renders').first.fetch('mwan_transfer_expected_interruptions')
  end

  def manifest(variables, interruptions)
    checksum = '1' * 64
    files = { 'results' => Array.new(3) { { 'stat' => { 'checksum' => checksum } } } }
    inputs = {
      'mwan_operation_guest_machine' => { 'stdout' => 'guest-machine' },
      'mwan_operation_guest_boot' => { 'stdout' => 'guest-boot' },
      'mwan_operation_host_machine' => { 'stdout' => 'hypervisor-machine' },
      'mwan_operation_baseline_files' => files, 'mwan_operation_target_files' => files,
      'mwan_operation_observer_identities' => { 'results' => [] },
      'mwan_deploy_trace_id' => 'manifest-fixture', 'mwan_operation_generation' => 'generation',
      'actual_vmid' => 113, 'mwan_pre_deploy_snapshot_name' => 'snapshot',
      'mwan_operation_deadline' => { 'stdout' => '2026-10-02T20:00:00Z' },
      'mwan_transfer_expected_interruptions' => interruptions
    }
    checks = %w[webpass att].flat_map do |connection|
      %w[ipv4 ipv6].map do |family|
        { 'id' => "inbound-#{connection}-#{family}", 'dimension' => 'inbound_application',
          'operation' => 'http', 'family' => family, 'observer_vmid' => 0, 'interface' => 'eth0',
          'source' => family == 'ipv4' ? '192.0.2.1' : '2001:db8::1',
          'target' => family == 'ipv4' ? 'http://192.0.2.2/cf_check' : 'http://[2001:db8::2]/cf_check' }
      end
    end
    inputs.merge!('mwan_operation_inbound_checks' => checks, 'mwan_operation_restored_inbound_checks' => checks,
                  'mwan_operation_downstream_checks' => [], 'mwan_operation_restored_downstream_checks' => [])
    template = File.read(File.join(root, 'mwan/config/deploy-operation.json.j2'))
    result = TaskExpressions.evaluate(
      variables: {}, facts: [],
      renders: [TaskExpressions.render_task({ 'vars' => variables.merge(inputs) }, 'manifest' => template)]
    )
    JSON.parse(result.fetch('renders').first.fetch('manifest'))
  end

  it 'serializes only the selected inbound exemptions without adding provider path claims' do
    variables = settings
    selected = interruptions(variables)
    expect(selected).to eq([{ 'phase' => 'connection-handover-webpass',
                              'check_ids' => %w[inbound-webpass-ipv4 inbound-webpass-ipv6], 'max_seconds' => 7735 }])
    document = manifest(variables, selected)
    expect(document.fetch('expected_interruptions')).to eq(selected)
    expect(document.fetch('required_checks').map { |check| check.fetch('id') }).to contain_exactly(
      'inbound-webpass-ipv4', 'inbound-webpass-ipv6', 'inbound-att-ipv4', 'inbound-att-ipv6'
    )
    document.fetch('required_checks').each do |check|
      expect(check).not_to have_key('connection_id')
      expect(check.fetch('interface')).to eq('eth0')
      expect(check.fetch('source')).not_to be_empty
    end
    expect(document).not_to have_key('mwan_operation_inbound_check_connections')
  end

  it 'serializes no exemptions for nonproviders or connections without associated inbound checks' do
    preparation = settings(transfer: false)
    document = manifest(preparation, preparation.fetch('mwan_transfer_expected_interruptions'))
    expect(document.fetch('expected_interruptions')).to eq([])
    expect(document.fetch('recovery_timeout_seconds')).to eq(2700)
    variables = settings
    variables['mwan_transfer_provider'] = false
    expect(interruptions(variables)).to eq([])
    variables['mwan_transfer_provider'] = true
    variables['mwan_transfer_connection_id'] = 'management'
    expect(interruptions(variables)).to eq([])
    expect(manifest(variables, []).fetch('expected_interruptions')).to eq([])
  end

  it 'rejects association keys or stable connections absent from the declared contract' do
    variables = settings.merge('mwan_transfer_target_document' => {
                                 'ietf-interfaces:interfaces' => { 'interface' => [
                                   { 'goodkind-mwan-steering:connection-id' => 'webpass' },
                                   { 'goodkind-mwan-steering:connection-id' => 'att' }
                                 ] }
                               })
    assertion = task('Require explicit inbound check associations to configured connections')
    templates = assertion.fetch('ansible.builtin.assert').fetch('that').each_with_index.to_h do |expression, index|
      [index.to_s, "{{ #{expression} }}"]
    end
    [{ 'key' => 'inbound-webpass-ipv4', 'value' => 'webpass' },
     { 'key' => 'downstream-a-ipv4', 'value' => 'webpass' },
     { 'key' => 'inbound-webpass-ipv4', 'value' => 'missing' }].each_with_index do |association, index|
      result = TaskExpressions.evaluate(
        variables: {}, facts: [],
        renders: [TaskExpressions.render_task({ 'vars' => variables.merge(assertion.fetch('vars')).merge('item' => association) }, templates)]
      )
      expect(result.fetch('renders').first.values.all?(true)).to eq(index.zero?)
    end
  end
end
