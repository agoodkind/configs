# frozen_string_literal: true

require 'fileutils'
require 'ipaddr'
require 'json'
require 'tmpdir'
require_relative '../support/ansible_render'

# Every suburban guest takes a direct SSH connection built from the shared base
# arguments, and the router is reached at its routed IPv6 transit address. These
# checks render the real inventory inputs and read each host's values back.
module SuburbanInventory
  INVENTORY_INPUTS = %w[
    service_mapping.yml
    group_vars/all/service_mapping.yml
    group_vars/all/vars.yml
  ].freeze
  EXPECTED_HOSTS = %w[
    tack-qa.suburban.goodkind.io
    seaweedfs.suburban.goodkind.io
    dns64.suburban.goodkind.io
    mwan.suburban.goodkind.io
    mwan-failover.suburban.goodkind.io
    router.suburban.goodkind.io
  ].freeze
  ROUTER_HOSTNAME = 'router.suburban.goodkind.io'
  MULTICAST_IPV6 = IPAddr.new('ff00::/8')

  module_function

  def uses_indirect_ssh?(arguments)
    lower_arguments = arguments.downcase
    return true if lower_arguments.include?('proxyjump')
    return true if lower_arguments.include?('proxycommand')

    arguments.split.any? { |field| field.start_with?('-J') }
  end

  # A routed address is a single global unicast IPv6 address that is not an
  # IPv4-mapped one. A prefix or a hostname is not an address.
  def routed_ipv6?(address)
    return false if address.include?('/')

    parsed_address = IPAddr.new(address)
    return false unless parsed_address.ipv6?
    return false if parsed_address.ipv4_mapped?

    global_unicast_ipv6?(parsed_address)
  rescue IPAddr::Error
    false
  end

  def global_unicast_ipv6?(parsed_address)
    return false if parsed_address.to_i.zero?
    return false if parsed_address.loopback?
    return false if MULTICAST_IPV6.include?(parsed_address)

    !parsed_address.link_local?
  end

  def render(work_directory)
    inventory_directory = File.join(work_directory, 'inventory')
    rendered_directory = File.join(work_directory, 'rendered')
    FileUtils.mkdir_p(rendered_directory)
    INVENTORY_INPUTS.each do |input_path|
      destination = File.join(inventory_directory, input_path)
      FileUtils.mkdir_p(File.dirname(destination))
      FileUtils.cp(File.join(AnsibleRender::REPOSITORY_ROOT, 'ansible', 'inventory', input_path), destination)
    end
    AnsibleRender.render(
      inventory: inventory_directory,
      playbook: 'render_inventory.yml',
      extra_vars: { 'output_directory' => rendered_directory, 'ansible_become' => false }
    )
    rendered_directory
  end

  def rendered_host(rendered_directory, hostname)
    JSON.parse(File.read(File.join(rendered_directory, "#{hostname}.json")))
  end

  def rendered_string(rendered, key)
    value = rendered.fetch(key)
    return '' if value.nil?
    raise "#{key} = #{value.inspect}, want a string" unless value.is_a?(String)

    value
  end
end

RSpec.describe SuburbanInventory do
  describe '.uses_indirect_ssh?' do
    {
      'direct' => ['-o StrictHostKeyChecking=no', false],
      'ProxyJump' => ['-o ProxyJump=proxy', true],
      'attached ProxyJump' => ['-oProxyJump=proxy', true],
      'separate short option' => ['-J proxy', true],
      'attached short option' => ['-Jproxy', true],
      'ProxyCommand' => ["-o ProxyCommand='ssh proxy'", true]
    }.each do |name, (arguments, expected)|
      it "returns #{expected} for #{name}" do
        expect(described_class.uses_indirect_ssh?(arguments)).to be(expected)
      end
    end
  end

  describe '.routed_ipv6?' do
    {
      'routed IPv6' => ['3d06:bad:b01:201::2', true],
      'IPv4' => ['192.0.2.1', false],
      'mapped IPv4' => ['::ffff:192.0.2.1', false],
      'link local IPv6' => ['fe80::1', false],
      'hostname' => ['router.suburban.goodkind.io', false],
      'empty' => ['', false]
    }.each do |name, (address, expected)|
      it "returns #{expected} for #{name}" do
        expect(described_class.routed_ipv6?(address)).to be(expected)
      end
    end
  end

  describe 'rendered suburban inventory' do
    before(:all) do
      @work_directory = Dir.mktmpdir('suburban-inventory')
      @rendered_directory = described_class.render(@work_directory)
    end

    after(:all) do
      FileUtils.remove_entry(@work_directory)
    end

    SuburbanInventory::EXPECTED_HOSTS.each do |hostname|
      it "connects to #{hostname} over direct SSH", :aggregate_failures do
        rendered = described_class.rendered_host(@rendered_directory, hostname)
        ssh_arguments = described_class.rendered_string(rendered, 'ansible_ssh_common_args')
        base_arguments = described_class.rendered_string(rendered, 'ssh_base_args')

        expect(ssh_arguments).to eq(base_arguments),
                                 "ansible_ssh_common_args = #{ssh_arguments.inspect}, want shared ssh_base_args #{base_arguments.inspect}"
        expect(described_class.uses_indirect_ssh?(ssh_arguments)).to be(false),
                                                                     "SSH args use an indirect connection: #{ssh_arguments.inspect}"
      end
    end

    it 'targets the router at its routed IPv6 transit address', :aggregate_failures do
      rendered = described_class.rendered_host(@rendered_directory, SuburbanInventory::ROUTER_HOSTNAME)
      ansible_host = described_class.rendered_string(rendered, 'ansible_host')
      router_transit = described_class.rendered_string(rendered, 'router_ipv6_transit')

      expect(described_class.routed_ipv6?(ansible_host)).to be(true),
                                                            "router ansible_host = #{ansible_host.inspect}, want routed IPv6 literal"
      expect(ansible_host).to eq(router_transit),
                              "router ansible_host = #{ansible_host.inspect}, want routed IPv6 #{router_transit.inspect}"
    end
  end
end
