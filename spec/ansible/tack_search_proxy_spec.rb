# frozen_string_literal: true

require 'yaml'
require_relative '../support/tack_search_inventory'

# These examples read the Traefik configuration and systemd unit that a
# deploy renders for the search endpoint on each hypervisor.
module TackSearchProxy
  TASKS_FILE = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks', 'tasks', 'tack-search-proxy.yml')
  PROXY_DIRECTORY = '/etc/tack-search-proxy'
  SECRET_TASKS = [
    'Install the Tack search proxy private key',
    'Render the Tack search proxy configuration'
  ].freeze

  module_function

  def load_balancer(rendered)
    rendered.yaml(rendered.hypervisor, 'proxy').dig('http', 'services', 'tack-search', 'loadBalancer')
  end

  def backend_urls(rendered)
    load_balancer(rendered).fetch('servers').map { |server| server.fetch('url') }
  end

  def tasks
    YAML.safe_load_file(TASKS_FILE)
  end
end

RSpec.describe TackSearchProxy do
  let(:service_mapping) do
    YAML.safe_load_file(File.join(TackSearchInventory::INVENTORY_DIRECTORY, TackSearchInventory::MAPPING_FILE), aliases: true)
        .fetch('service_mapping')
  end

  # tack_search1 is the committed guest at ::125. The render adds tack_search2
  # at ::126 and tack_search3 at ::127.
  {
    1 => %w[https://[3d06:bad:b01::125]:9200],
    3 => %w[https://[3d06:bad:b01::125]:9200 https://[3d06:bad:b01::126]:9200 https://[3d06:bad:b01::127]:9200]
  }.each do |member_count, urls|
    it "renders one production backend per member for #{member_count} member(s)" do
      rendered = TackSearchInventory.rendered(:production, member_count: member_count)

      expect(described_class.backend_urls(rendered)).to eq(urls)
    end
  end

  {
    production: { callers: %w[tack tack_app2], listen: '[3d06:bad:b01::254]:9200', prefix: 'vault_tack' },
    qa: { callers: %w[tack_qa_suburban tack_app2_suburban], listen: '[3d06:bad:b01:210::5]:9200', prefix: 'vault_tack_qa' }
  }.each do |environment, expected|
    describe "the #{environment} endpoint" do
      let(:rendered) { TackSearchInventory.rendered(environment) }
      let(:proxy) { rendered.yaml(rendered.hypervisor, 'proxy') }

      it 'listens on the hypervisor search address with the pinned Traefik release' do
        exec_start = rendered.file(rendered.hypervisor, 'proxy_service').lines.grep(/\AExecStart=/).first

        expect(exec_start.split).to include(
          '--entryPoints.search.address=' + expected.fetch(:listen),
          "--providers.file.filename=#{TackSearchProxy::PROXY_DIRECTORY}/traefik.yml"
        )
        expect(exec_start).to start_with('ExecStart=/usr/local/lib/tack-search-proxy/traefik-3.0.0/traefik ')
      end

      it 'admits only the application guests and their container subnets' do
        callers = expected.fetch(:callers).flat_map do |caller|
          entry = service_mapping.fetch(caller)
          ["#{entry.fetch('ipv6')}/128", entry.fetch('docker_v6_subnet')]
        end

        expect(proxy.dig('http', 'routers', 'tack-search', 'middlewares')).to eq(['tack-search-callers'])
        expect(proxy.dig('http', 'middlewares', 'tack-search-callers', 'ipAllowList', 'sourceRange')).to eq(callers)
      end

      it 'terminates TLS with the proxy certificate and verifies every member against the search authority' do
        load_balancer = described_class.load_balancer(rendered)
        transport = proxy.dig('http', 'serversTransports', load_balancer.fetch('serversTransport'))

        expect(proxy.dig('tls', 'stores', 'default', 'defaultCertificate')).to eq(
          'certFile' => "#{TackSearchProxy::PROXY_DIRECTORY}/server.crt",
          'keyFile' => "#{TackSearchProxy::PROXY_DIRECTORY}/server.key"
        )
        expect(proxy.dig('http', 'routers', 'tack-search', 'tls')).to eq({})
        expect(transport).to eq('rootCAs' => ["#{TackSearchProxy::PROXY_DIRECTORY}/ca.crt"])
        expect(described_class.backend_urls(rendered)).to all(start_with('https://'))
      end

      it 'checks readiness with an authenticated cluster-health request' do
        health_check = described_class.load_balancer(rendered).fetch('healthCheck')
        prefix = expected.fetch(:prefix)
        credentials = "render-only-#{prefix}_search_username:render-only-#{prefix}_search_password"

        expect(health_check.fetch('path')).to eq('/_cluster/health')
        expect(health_check.dig('headers', 'Authorization')).to eq("Basic #{[credentials].pack('m0')}")
      end
    end
  end

  it 'writes the proxy secrets readable only by root and does not log them', :aggregate_failures do
    TackSearchProxy::SECRET_TASKS.each do |name|
      task = described_class.tasks.find { |candidate| candidate['name'] == name }
      module_arguments = task.fetch(task.keys.find { |key| key.start_with?('ansible.builtin.') })

      expect(task['no_log']).to be(true), "#{name} logs its content"
      expect(module_arguments).to include('owner' => 'root', 'mode' => '0600')
    end
  end
end
