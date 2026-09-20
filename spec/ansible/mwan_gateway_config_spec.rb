# frozen_string_literal: true

require 'json'
require 'tmpdir'
require 'yaml'
require_relative '../support/ansible_render'
require_relative '../support/command_runner'

# `mwan install --role wan --apply` renders /etc/sysctl.d/99-mwan.conf,
# /etc/iproute2/rt_tables and /etc/nghttpx/wanconfig.conf from
# /etc/mwan/config.toml and /etc/mwan/network.json (MWAN-382). The deploy no
# longer templates any of the three, so a key missing from the rendered
# config.toml makes the verb skip a file and the gateway keeps whatever an
# earlier deploy left there. These checks render the real template against the
# real group_vars for both gateways and read the four keys back with a TOML
# parser, so a missing key, a wrong kernel interface name, or a reserved table
# that never reached the file fails the pull request.
module MwanGatewayConfig
  INVENTORY_DIRECTORY = File.join(AnsibleRender::REPOSITORY_ROOT, 'ansible', 'inventory')
  GROUP_VARS_DIRECTORY = File.join(INVENTORY_DIRECTORY, 'group_vars')
  SERVICE_MAPPING_FILE = File.join(GROUP_VARS_DIRECTORY, 'all', 'service_mapping.yml')
  SHARED_VARS_FILE = File.join(GROUP_VARS_DIRECTORY, 'all', 'vars.yml')
  TEMPLATE_FILE = File.join(AnsibleRender::REPOSITORY_ROOT, 'mwan', 'config', 'config-vm.toml.j2')
  TOML_READER = File.join(AnsibleRender::FIXTURE_DIRECTORY, 'read_toml.py')

  # The two gateways this template renders for, named by the group_vars file
  # that carries each one's interface names and AT&T VLAN id.
  ENVIRONMENTS = {
    'production' => 'mwan_servers.yml',
    'testbed' => 'mwan_suburban_servers.yml'
  }.freeze

  # Values the vaulted inventory carries. The play loads no vault, and no key
  # under test is a secret, so each one arrives as a placeholder.
  VAULT_PLACEHOLDER = 'spec-placeholder'
  VAULT_VARIABLES = %w[
    vault_smtp2go_api_key
    vault_mwan_watchdog_pve_token_secret
    vault_suburban_testbed_pve_token_secret
    vault_prod_opnsense_api_key
    vault_prod_opnsense_api_secret
    vault_suburban_testbed_opnsense_api_key
    vault_suburban_testbed_opnsense_api_secret
  ].freeze

  module_function

  def group_vars_path(group_file)
    File.join(GROUP_VARS_DIRECTORY, group_file)
  end

  # The inventory as the deploy layers it for one gateway: the shared files
  # first, the environment's group file last.
  def inventory_values(group_file)
    [SERVICE_MAPPING_FILE, SHARED_VARS_FILE, group_vars_path(group_file)].reduce({}) do |merged, path|
      merged.merge(YAML.safe_load_file(path, aliases: true))
    end
  end

  def fetch_value(values, name)
    raise "the gateway inventory does not set #{name}" unless values.key?(name)

    values.fetch(name)
  end

  # The AT&T link as the kernel names it: the parent interface on a gateway
  # whose inventory carries no VLAN id, and the tagged link where it does.
  def att_link(values)
    parent = fetch_value(values, 'mwan_att_iface')
    vlan_id = fetch_value(values, 'mwan_att_vlan_id')
    return parent.to_s if vlan_id.to_s.empty?

    "#{parent}.#{vlan_id}"
  end

  # Renders config-vm.toml.j2 for one gateway and returns the document a TOML
  # parser reads back out of it.
  def rendered_config(group_file)
    Dir.mktmpdir('mwan-config') do |output_directory|
      output_file = File.join(output_directory, 'config.toml')
      AnsibleRender.render(
        inventory: 'localhost,',
        playbook: 'render_mwan_config.yml',
        extra_vars: vault_values.merge(
          'service_mapping_file' => SERVICE_MAPPING_FILE,
          'shared_vars_file' => SHARED_VARS_FILE,
          'group_vars_file' => group_vars_path(group_file),
          'template_file' => TEMPLATE_FILE,
          'output_file' => output_file
        )
      )
      parse_toml(File.read(output_file))
    end
  end

  def vault_values
    VAULT_VARIABLES.to_h { |name| [name, VAULT_PLACEHOLDER] }
  end

  def parse_toml(document)
    result = CommandRunner.capture(
      [AnsibleRender.ansible_python, TOML_READER],
      stdin_data: document,
      chdir: AnsibleRender::REPOSITORY_ROOT,
      timeout_seconds: AnsibleRender::PLAYBOOK_TIMEOUT_SECONDS
    )
    raise "read the rendered config.toml exceeded #{AnsibleRender::PLAYBOOK_TIMEOUT_SECONDS}s" if result.timed_out
    raise "read the rendered config.toml: #{result.exit_status}\n#{result.error_output}" unless result.exit_status.success?

    JSON.parse(result.output)
  end
end

RSpec.describe MwanGatewayConfig do
  MwanGatewayConfig::ENVIRONMENTS.each do |environment, group_file|
    describe "the #{environment} gateway" do
      # One render per environment, because each one runs a playbook.
      before(:context) do
        @values = described_class.inventory_values(group_file)
        @config = described_class.rendered_config(group_file)
      end

      it 'carries the RESTCONF port the front-end configuration is rendered from' do
        want = described_class.fetch_value(@values, 'wanconfig_restconf_port')

        expect(@config.dig('wanconfig', 'restconf_port')).to eq(want),
                                                             'without [wanconfig] restconf_port the install verb ' \
                                                             'skips /etc/nghttpx/wanconfig.conf and the RESTCONF ' \
                                                             'surface keeps the port an earlier deploy wrote'
      end

      it 'carries every reserved routing table the routing table file lists' do
        want = described_class.fetch_value(@values, 'mwan_reserved_tables')

        expect(@config.dig('routing', 'reserved_tables')).to eq(want),
                                                             'without [routing.reserved_tables] the install verb ' \
                                                             'skips /etc/iproute2/rt_tables, and a table missing ' \
                                                             'from it drops a name the operator types'
      end

      it 'names the interfaces whose router advertisements the kernel ignores' do
        want = [described_class.fetch_value(@values, 'mwan_webpass_iface')]

        expect(@config.dig('sysctl', 'disable_slaac_ifaces')).to eq(want),
                                                                 'the gateway takes unwanted autoconfigured ' \
                                                                 'addresses on any link left out of this list'
      end

      it 'names the interfaces whose reverse path filtering is off, as the kernel names them' do
        want = [
          described_class.fetch_value(@values, 'mwan_webpass_iface'),
          described_class.att_link(@values)
        ]

        expect(@config.dig('sysctl', 'disable_rp_filter_ifaces')).to eq(want),
                                                                     'a name the kernel does not have leaves ' \
                                                                     'reverse path filtering on, and policy routing ' \
                                                                     "then drops that provider's return traffic"
      end
    end
  end
end
