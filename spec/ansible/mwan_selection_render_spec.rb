# frozen_string_literal: true

require 'json'
require 'tmpdir'
require 'yaml'
require_relative '../support/ansible_render'

RSpec.describe 'MWAN connection selection configuration' do
  let(:root) { AnsibleRender::REPOSITORY_ROOT }
  let(:providers) do
    YAML.safe_load_file(File.join(root, 'ansible/inventory/group_vars/mwan_suburban_servers.yml')).fetch('mwan_providers')
  end

  def run_mwan(binary, *arguments)
    result = CommandRunner.run({}, [binary, *arguments], chdir: root, timeout_seconds: 60)
    expect(result.timed_out).to be(false), result.output
    expect(result.exit_status.success?).to be(true), result.output
  end

  it 'validates omitted, enabled, and excluded connections through the real loader' do
    binary = ENV['MWAN_TRANSLATION_TEST_BINARY']
    skip 'Set MWAN_TRANSLATION_TEST_BINARY to a Linux mwan executable with connection selection support' unless binary

    binary = File.expand_path(binary, root)
    Dir.mktmpdir('mwan-selection-render') do |directory|
      schema = File.join(directory, 'schema')
      Dir.mkdir(schema)
      run_mwan(binary, 'install', '--print-schema', schema)

      [nil, true, false].each do |enabled|
        configured = providers.map(&:dup)
        webpass = configured.find { |provider| provider.fetch('connection_id') == 'webpass' }
        webpass['selection_enabled'] = enabled unless enabled.nil?
        network = File.join(directory, 'network.json')
        AnsibleRender.render(
          inventory: 'localhost,', playbook: 'render_mwan_network.yml',
          extra_vars: { 'repository_root' => root, 'network_output' => network, 'mwan_providers' => configured }
        )
        interfaces = JSON.parse(File.read(network)).fetch('ietf-interfaces:interfaces').fetch('interface')
        interfaces.select { |entry| entry.key?('goodkind-mwan-steering:steering') }.each do |entry|
          steering = entry.fetch('goodkind-mwan-steering:steering')
          if entry.fetch('goodkind-mwan-steering:connection-id') == 'webpass' && !enabled.nil?
            expect(steering.fetch('enabled')).to be(enabled)
          else
            expect(steering).not_to have_key('enabled')
          end
        end
        run_mwan(binary, 'deploy-gate', 'check-network', network, schema)
      end
    end
  end
end
