# frozen_string_literal: true

require 'fileutils'
require 'tmpdir'
require 'yaml'
require_relative '../support/tack_search_inventory'

# The spec checks the rendered limits to detect a missing testbed inventory
# override.
module MwanHostRuntimeRender
  TEMPLATE = 'mwan/config/config-host.toml.j2'
  TEMPLATE_NAME = 'config-host.toml'
  HOSTS = %w[vault suburban].freeze
  SECRET_NAMES = %w[
    vault_smtp2go_api_key vault_mwan_watchdog_pve_token_secret vault_suburban_testbed_pve_token_secret
  ].freeze

  module_function

  def rendered
    @rendered ||= render
  end

  def render
    work_directory = Dir.mktmpdir('mwan-host-runtime-render')
    at_exit { FileUtils.remove_entry(work_directory) }
    inventory_directory = TackSearchInventory.copy_inventory(work_directory)
    write_placeholder_secrets(inventory_directory)
    output_directory = File.join(work_directory, 'rendered')
    FileUtils.mkdir_p(output_directory)
    AnsibleRender.render(
      inventory: inventory_directory,
      playbook: TackSearchInventory::PLAYBOOK,
      extra_vars: {
        'render_hosts' => HOSTS.join(','),
        'render_templates' => HOSTS.to_h { |host| [host, [{ 'name' => TEMPLATE_NAME, 'src' => TEMPLATE }]] },
        'repository_root' => AnsibleRender::REPOSITORY_ROOT,
        'output_directory' => output_directory,
        'ansible_become' => false
      }
    )
    output_directory
  end

  # The template reads these vault variables. The placeholder vault gives each
  # one a value that contains the variable's own name.
  def write_placeholder_secrets(inventory_directory)
    vault_path = File.join(inventory_directory, TackSearchInventory::VAULT_FILE)
    secrets = YAML.safe_load_file(vault_path).merge(SECRET_NAMES.to_h { |name| [name, "render-only-#{name}"] })
    File.write(vault_path, YAML.dump(secrets))
  end

  def watchdog_setting(host, key)
    File.read(File.join(rendered, "#{host}-#{TEMPLATE_NAME}")).lines(chomp: true).grep(/\A#{key} = /)
  end
end

RSpec.describe MwanHostRuntimeRender do
  it 'keeps the production total snapshot limit on vault' do
    expect(described_class.watchdog_setting('vault', 'max_total_snapshots')).to eq(['max_total_snapshots = 15'])
  end

  it 'turns off total-count snapshot pruning on the suburban testbed' do
    expect(described_class.watchdog_setting('suburban', 'max_total_snapshots')).to eq(['max_total_snapshots = 0'])
  end
end
