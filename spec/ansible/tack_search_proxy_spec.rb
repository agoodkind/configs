# frozen_string_literal: true

require 'yaml'

REPOSITORY_ROOT = File.expand_path('../..', __dir__)
PROXY_TEMPLATE = File.join(REPOSITORY_ROOT, 'proxmox', 'config', 'tack-search-proxy.yml.j2')
PROXY_TASKS = File.join(REPOSITORY_ROOT, 'ansible', 'playbooks', 'tasks', 'tack-search-proxy.yml')

RSpec.describe 'Tack OpenSearch proxy configuration' do
  it 'defines TLS, authenticated routing, and re-encrypted backends' do
    template = File.read(PROXY_TEMPLATE)

    expect(template).to include('middlewares: [tack-search-auth]')
    expect(template).to include('usersFile: /etc/tack-search-proxy/users')
    expect(template).to include('rootCAs:')
    expect(template).to include('serverName: opensearch')
    expect(template).to include('minVersion: VersionTLS13')
  end

  it 'installs proxy credentials without logging secret values' do
    tasks = File.read(PROXY_TASKS)

    expect(tasks).to include('tack_search_proxy_users_file')
    expect(tasks).to include('no_log: true')
    expect(tasks).to include('mode: "0600"')
  end

  it 'keeps the proxy service on the stable search port' do
    service = File.read(File.join(REPOSITORY_ROOT, 'proxmox', 'services', 'tack-search-proxy.service.j2'))

    expect(service).to include('entryPoints.search.address=')
    expect(service).to include('{{ tack_search_proxy_port }}')
    expect(service).to include('providers.file.filename=/etc/tack-search-proxy/traefik.yml')
  end
end
