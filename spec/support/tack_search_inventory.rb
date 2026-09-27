# frozen_string_literal: true

require 'fileutils'
require 'tmpdir'
require 'yaml'
require_relative 'ansible_render'

# Renders the Tack search templates for real inventory hosts. Each render
# copies the repository inventory, replaces the encrypted vault with
# placeholders named after the vault variables the templates read, and can
# add test-only production members, so every file resolves its group
# variables the way a deploy does.
module TackSearchInventory
  INVENTORY_DIRECTORY = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'inventory')
  COPIED_ENTRIES = %w[hosts service_mapping.yml group_vars].freeze
  VAULT_FILE = File.join('group_vars', 'all', 'vault.yml')
  MAPPING_FILE = File.join('group_vars', 'all', 'service_mapping.yml')
  CLUSTER_FILE = File.join('group_vars', 'all', 'search_cluster.yml')
  PLAYBOOK = 'render_tack_search.yml'
  APP_PASSWORD = 'render-only-app-login'
  TEMPLATES = {
    'env' => 'tack/tack.env.j2',
    'override' => 'tack/docker-compose.override.yml.j2',
    'users' => 'tack/opensearch-internal-users.yml.j2',
    'roles_mapping' => 'tack/opensearch-roles-mapping.yml.j2',
    'proxy' => 'proxmox/config/tack-search-proxy.yml.j2',
    'proxy_service' => 'proxmox/services/tack-search-proxy.service.j2'
  }.freeze
  # The owner guest, the hypervisor, and the member name prefix of each
  # environment.
  ENVIRONMENTS = {
    production: { owner: 'tack.home.goodkind.io', hypervisor: 'vault', domain: 'home.goodkind.io' },
    qa: { owner: 'tack-qa.suburban.goodkind.io', hypervisor: 'suburban', domain: 'suburban.goodkind.io' }
  }.freeze
  SECRET_NAMES = %w[
    yugabyte_password audit_writer_password audit_reader_password audit_redactor_password
    audit_operator_password search_username search_password search_password_hash search_ca
    search_proxy_certificate search_proxy_private_key search_cursor_key
  ].freeze
  SHARED_SECRETS = %w[vault_seaweedfs_s3_access_key vault_seaweedfs_s3_secret_key].freeze

  Rendered = Struct.new(:directory, :environment, :members, keyword_init: true) do
    def file(host, name)
      File.read(File.join(directory, "#{host}-#{name}"))
    end

    def settings(host)
      pairs = file(host, 'env').lines(chomp: true).grep(/\A[A-Z][A-Z0-9_]*=/)
      pairs.to_h { |line| line.split('=', 2) }
    end

    def yaml(host, name)
      YAML.safe_load(file(host, name))
    end

    def owner = ENVIRONMENTS.fetch(environment).fetch(:owner)
    def hypervisor = ENVIRONMENTS.fetch(environment).fetch(:hypervisor)

    def member_host(member)
      "#{member.tr('_', '-').delete_suffix('-suburban')}.#{ENVIRONMENTS.fetch(environment).fetch(:domain)}"
    end
  end

  module_function

  # One render per environment, member count, and set of overrides, shared by
  # every spec file in the run and removed when the run ends. Each override is
  # an extra variable that replaces the committed inventory value, such as
  # tack_search_enabled or tack_search_public_enabled.
  def rendered(environment, member_count: 1, overrides: {})
    @rendered ||= {}
    @rendered[[environment, member_count, overrides]] ||= render(environment, member_count, overrides)
  end

  def render(environment, member_count, overrides)
    work_directory = Dir.mktmpdir('tack-search-render')
    at_exit { FileUtils.remove_entry(work_directory) }
    inventory_directory = copy_inventory(work_directory)
    members = members_for(environment, member_count)
    add_production_members(inventory_directory, members) if environment == :production
    output_directory = File.join(work_directory, 'rendered')
    FileUtils.mkdir_p(output_directory)
    result = Rendered.new(directory: output_directory, environment: environment, members: members)
    run(inventory_directory, output_directory, host_templates(result), overrides)
    result
  end

  def members_for(environment, member_count)
    return ['tack_search1_suburban'] if environment == :qa

    (1..member_count).map { |number| "tack_search#{number}" }
  end

  def host_templates(result)
    templates = { result.owner => %w[env], result.hypervisor => %w[proxy proxy_service] }
    result.members.each { |member| templates[result.member_host(member)] = %w[override users roles_mapping env] }
    templates.transform_values { |names| names.map { |name| { 'name' => name, 'src' => TEMPLATES.fetch(name) } } }
  end

  def copy_inventory(work_directory)
    inventory_directory = File.join(work_directory, 'inventory')
    FileUtils.mkdir_p(inventory_directory)
    COPIED_ENTRIES.each do |entry|
      FileUtils.cp_r(File.join(INVENTORY_DIRECTORY, entry), inventory_directory)
    end
    File.write(File.join(inventory_directory, VAULT_FILE), YAML.dump(placeholder_secrets))
    inventory_directory
  end

  # Every vault variable the rendered files read, set to a value that names it.
  def placeholder_secrets
    names = SHARED_SECRETS.dup
    SECRET_NAMES.each { |name| names.push("vault_tack_#{name}", "vault_tack_qa_#{name}") }
    names.to_h { |name| [name, "render-only-#{name}"] }
  end

  # Test-only production members beyond tack_search1, each with its own
  # address and group, listed in the production member list.
  def add_production_members(inventory_directory, members)
    mapping_path = File.join(inventory_directory, MAPPING_FILE)
    mapping = YAML.safe_load_file(mapping_path, aliases: true)
    members.drop(1).each_with_index do |member, index|
      suffix = 126 + index
      mapping.fetch('service_mapping')[member] = {
        'hostname' => "#{member.tr('_', '-')}.home.goodkind.io", 'vmid' => suffix,
        'ipv6' => "3d06:bad:b01::#{suffix}", 'mac_address' => format('BC:24:11:A3:53:%02X', suffix),
        'docker_v6_subnet' => "3d06:bad:b01:0:7c#{index}::/96", 'docker_v6_gateway' => "3d06:bad:b01:0:7c#{index}::1"
      }
      %w[tack_all tack_prod_all tack_search].each { |group| mapping.fetch('group_children').fetch(group).push("#{member}_servers") }
    end
    File.write(mapping_path, YAML.dump(mapping))
    cluster_path = File.join(inventory_directory, CLUSTER_FILE)
    cluster = YAML.safe_load_file(cluster_path)
    File.write(cluster_path, YAML.dump(cluster.merge('tack_search_production_members' => members)))
  end

  def run(inventory_directory, output_directory, templates, overrides)
    AnsibleRender.render(
      inventory: inventory_directory,
      playbook: PLAYBOOK,
      extra_vars: {
        'render_hosts' => templates.keys.join(','),
        'render_templates' => templates,
        'repository_root' => AnsibleRender::REPOSITORY_ROOT,
        'output_directory' => output_directory,
        'tack_app_password' => APP_PASSWORD,
        'ansible_become' => false
      }.merge(overrides)
    )
  end
end
