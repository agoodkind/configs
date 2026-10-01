# frozen_string_literal: true

require 'json'
require 'shellwords'
require 'tmpdir'
require_relative '../support/ansible_render'

RSpec.describe 'MWAN role activation with actual systemd' do
  before do
    @physical_port = ENV.fetch('MWAN_ROLE_ACTIVATION_TEST_SSH_PORT', nil)
    @container = ENV.fetch('MWAN_ROLE_ACTIVATION_TEST_CONTAINER', nil)
    @container = 'physical_gateway' if @physical_port
    skip 'Set the isolated systemd container or physical SSH fixture' unless @container
    @physical_key = ENV.fetch('MWAN_ROLE_ACTIVATION_TEST_SSH_KEY') if @physical_port
    @physical_known_hosts = ENV.fetch('MWAN_ROLE_ACTIVATION_TEST_KNOWN_HOSTS') if @physical_port
  end

  def guest(*arguments)
    command = ['docker', 'exec', @container, *arguments]
    if @physical_port
      command = ['ssh', '-i', @physical_key, '-p', @physical_port, '-o', 'BatchMode=yes',
                 '-o', 'IdentitiesOnly=yes', '-o', "UserKnownHostsFile=#{@physical_known_hosts}",
                 'probe@127.0.0.1', "sudo -- #{Shellwords.join(arguments)}"]
    end
    result = CommandRunner.run({}, command,
                               chdir: AnsibleRender::REPOSITORY_ROOT, timeout_seconds: 30)
    raise "guest command failed: #{result.output}" unless result.exit_status.success? && !result.timed_out

    result.output
  end

  def activation_play(directory, phase, prior, current, **options)
    inventory = File.join(directory, 'inventory.ini')
    host = "#{@container} ansible_pipelining=true"
    if @physical_port
      host += " ansible_host=127.0.0.1 ansible_port=#{@physical_port} ansible_user=probe"
      host += " ansible_ssh_private_key_file=#{@physical_key}"
      host += " ansible_ssh_common_args='-o IdentitiesOnly=yes -o UserKnownHostsFile=#{@physical_known_hosts}'"
    end
    inventory_content = "[role_tests]\n#{host}\n"
    if @physical_port
      server = host.sub(@container, 'physical_server').sub("ansible_port=#{@physical_port}",
                                                           "ansible_port=#{ENV.fetch('MWAN_ROLE_ACTIVATION_TEST_SERVER_PORT')}")
      inventory_content += "\n[packet_peers]\n#{server}\n"
    end
    File.write(inventory, inventory_content)
    AnsibleRender.render(
      inventory: inventory, playbook: 'activate_mwan_roles.yml', timeout_seconds: 120,
      extra_vars: {
        'repository_root' => AnsibleRender::REPOSITORY_ROOT,
        'role_test_connection' => @physical_port ? 'ssh' : 'community.docker.docker',
        'role_test_become' => !@physical_port.nil?,
        'role_test_source_root' => ENV.fetch('MWAN_ROLE_ACTIVATION_SOURCE_ROOT', '/src'),
        'role_test_host' => @container, 'role_test_phase' => phase,
        'role_test_prior_network' => prior, 'role_test_current_network' => current,
        'role_test_prepared' => options.fetch(:prepared), 'role_test_corrupt_unit' => options.fetch(:corrupt),
        'mwan_dns_domain' => options.fetch(:domain), 'role_test_publish' => options.fetch(:publish, false),
        'mwan_transfer_packet_checks' => options.fetch(:packet_checks, [])
      }
    )
  rescue RuntimeError => e
    File.write(File.join(directory, "#{phase}.log"), e.message)
    evidence_directory = ENV.fetch('MWAN_ROLE_ACTIVATION_TEST_EVIDENCE', nil)
    if evidence_directory
      evidence_path = File.join(evidence_directory, "#{File.basename(directory)}-#{phase}.log")
      File.write(evidence_path, e.message)
    end
    raise
  end

  def role_state(name)
    JSON.parse(guest('networkctl', 'status', name, '--json=short'))
  end

  def packet_play(directory, checks)
    inventory = File.join(directory, 'packet-inventory.ini')
    File.write(inventory, "[role_tests]\n#{@container}\n")
    AnsibleRender.render(
      inventory: inventory, playbook: 'run_mwan_transfer_packets.yml',
      extra_vars: { 'repository_root' => AnsibleRender::REPOSITORY_ROOT,
                    'mwan_transfer_packet_checks' => checks }
    )
  end

  it 'resolves a ready operational IPv6 edge and rejects invalid runtime targets' do
    skip 'The actual NPT operational publisher fixture is required' unless ENV['MWAN_TRANSFER_PACKET_RUNTIME'] == '1'

    Dir.mktmpdir('mwan-transfer-packets') do |directory|
      check = {
        'phase' => 'selected', 'host' => @container, 'family' => 'ipv6',
        'connection_id' => 'webpass', 'description' => 'Current IPv6 translation endpoint reply',
        'argv' => ['ping', '-6', '-c', '1', '-W', '2', '2001:db8:ffff::1'],
        'ipv6_translation_edge' => { 'argument_index' => 6, 'prefix' => '', 'suffix' => '' }
      }
      packet_play(directory, [check])
      literal = check.reject { |key, _value| key == 'ipv6_translation_edge' }
      literal['argv'] = ['ping', '-6', '-c', '1', '-W', '2', '::1']
      packet_play(directory, [literal])
      expect do
        packet_play(directory, [check.merge('connection_id' => 'absent-connection')])
      end.to raise_error(RuntimeError, /exactly one operational connection identity/)
      expect do
        packet_play(directory, [check.merge('family' => 'ipv4')])
      end.to raise_error(RuntimeError, /An IPv6 translation edge requires an IPv6 packet check/)
      edge = check.fetch('ipv6_translation_edge').merge('argument_index' => 0)
      expect do
        packet_play(directory, [check.merge('ipv6_translation_edge' => edge)])
      end.to raise_error(RuntimeError, /An IPv6 translation edge requires an IPv6 packet check/)
    end
  end

  def with_real_network(legacy_transit: false, provider_transfer: false)
    Dir.mktmpdir('mwan-role-activation') do |directory|
      current = File.join(directory, 'current.json')
      prior = File.join(directory, 'prior.json')
      AnsibleRender.render(inventory: 'localhost,', playbook: 'render_mwan_network.yml',
                           extra_vars: { 'repository_root' => AnsibleRender::REPOSITORY_ROOT,
                                         'network_output' => current })
      document = JSON.parse(File.read(current))
      if @physical_port
        document.fetch('ietf-interfaces:interfaces').fetch('interface') << {
          'name' => 'enoob0', 'type' => 'iana-if-type:other',
          'goodkind-mwan-steering:connection-id' => 'fixture-control',
          'goodkind-mwan-steering:owner' => 'external'
        }
        firewall = document.fetch('ietf-interfaces:interfaces').fetch('goodkind-mwan-steering:steering-group')
                           .fetch('firewall')
        firewall['management-interface'] = 'enoob0'
        firewall['management-service'] = [
          { 'protocol' => 'tcp', 'port' => 22, 'allowed-source' => ['10.0.2.2/32'] },
          { 'protocol' => 'udp', 'port' => 68, 'allowed-source' => ['10.0.2.2/32'] }
        ]
        File.write(current, JSON.generate(document))
      end
      if legacy_transit
        document.fetch('ietf-interfaces:interfaces').fetch('interface').each do |entry|
          next unless %w[enmgmt0 enmwanbr0].include?(entry.fetch('name'))

          entry['goodkind-mwan-steering:link-files'] = 'hand-authored'
          entry.delete('goodkind-mwan-steering:link')
          entry.delete('ietf-ip:ipv4')
          entry.delete('ietf-ip:ipv6')
        end
      end
      File.write(prior, legacy_transit ? JSON.generate(document) : File.read(current))
      transfer_documents(prior, current) if provider_transfer
      prepared = guest('systemctl', 'show', 'mwan-ifmgr@wan', '--property=LoadState', '--value').strip == 'loaded'
      activation_play(directory, 'prepare', prior, current, prepared: prepared, corrupt: false,
                                                            domain: 'home.goodkind.io')
      yield directory, prior, current
    end
  end

  it 'verifies generated units before retiring legacy role files' do
    with_real_network do |directory, prior, current|
      preserved = guest('find', '/etc/systemd/network', '-type', 'f').lines.map(&:strip).reject do |path|
        %w[10-mgmt.network 40-mwanbr.network 20-enmgmt0.network 20-enmwanbr0.network].include?(File.basename(path))
      end.sort
      prior_files = guest('sha256sum', *preserved)
      activation_play(directory, 'activate', prior, current, prepared: true, corrupt: false,
                                                             domain: 'home.goodkind.io')
      expect(role_state('enmgmt0').fetch('NetworkFile')).to eq('/etc/systemd/network/20-enmgmt0.network')
      expect(role_state('enmwanbr0').fetch('NetworkFile')).to eq('/etc/systemd/network/20-enmwanbr0.network')
      expect(guest('find', '/etc/systemd/network', '-name', '10-mgmt.network', '-o', '-name', '40-mwanbr.network')).to eq('')
      expect(guest('sha256sum', *preserved)).to eq(prior_files)
    end
  end

  it 'retires recorded inputs and preserves unrelated networkd files and exclusion sentinels' do
    with_real_network do |directory, prior, current|
      guest('cp', '/etc/systemd/network/10-mgmt.network', '/etc/systemd/network/90-unrelated.network')
      guest('cp', '/etc/systemd/network/10-mgmt.network', '/etc/systemd/network/00-mwan-release-other.network')
      guest('cp', '/etc/systemd/network/10-mgmt.network', '/etc/systemd/network/91-retired.network')
      %w[90-unrelated.network 00-mwan-release-other.network 91-retired.network].each do |name|
        guest('sed', '-i', 's/^Name=.*/Name=unrelated0/', "/etc/systemd/network/#{name}")
      end
      manifest = File.join(directory, 'networkd-inputs.json')
      File.write(manifest, JSON.generate(['91-retired.network']))
      copy_to_guest(manifest, '/var/lib/mwan/networkd-inputs.json')

      preserved = guest('sha256sum', '/etc/systemd/network/90-unrelated.network',
                        '/etc/systemd/network/00-mwan-release-other.network')
      activation_play(directory, 'activate', prior, current, prepared: true, corrupt: false,
                                                             domain: 'home.goodkind.io')
      expect(guest('find', '/etc/systemd/network', '-name', '91-retired.network')).to eq('')
      expect(guest('sha256sum', '/etc/systemd/network/90-unrelated.network',
                   '/etc/systemd/network/00-mwan-release-other.network')).to eq(preserved)
    end
  end

  it 'preserves legacy inputs when the actual generated-unit verifier rejects a changed unit' do
    with_real_network do |directory, prior, current|
      legacy = guest('cat', '/etc/systemd/network/10-mgmt.network')
      expect do
        activation_play(directory, 'activate', prior, current, prepared: true, corrupt: true,
                                                               domain: 'home.goodkind.io')
      end.to raise_error(RuntimeError, /Verify the installed generated networkd units: .*non-zero return code/)
      expect(guest('cat', '/etc/systemd/network/10-mgmt.network')).to eq(legacy)
      expect(role_state('enmgmt0').fetch('NetworkFile')).to eq('/etc/systemd/network/10-mgmt.network')
    end
  end

  [false, true].each do |legacy_transit|
    it "restores observed role selection after activation fails with legacy_transit=#{legacy_transit}" do
      with_real_network(legacy_transit: legacy_transit) do |directory, prior, current|
        before = %w[enmgmt0 enmwanbr0].map { |name| role_state(name).fetch('NetworkFile') }
        legacy = guest('cat', '/etc/systemd/network/10-mgmt.network')
        expect do
          activation_play(directory, 'activate', prior, current, prepared: true,
                                                                 corrupt: false, domain: 'wrong.test')
        end.to raise_error(RuntimeError, /prior role inputs and applied state passed recovery verification/)
        expect(%w[enmgmt0 enmwanbr0].map { |name| role_state(name).fetch('NetworkFile') }).to eq(before)
        expect(guest('cat', '/etc/systemd/network/10-mgmt.network')).to eq(legacy)
        expect(JSON.parse(guest('cat', '/etc/mwan/network.json'))).to eq(JSON.parse(File.read(prior)))
      end
    end
  end

  %w[prefix metric].each do |field|
    it "rejects and recovers an applied role #{field} that differs from inventory" do
      with_real_network do |directory, prior, current|
        document = JSON.parse(File.read(current))
        interfaces = document.fetch('ietf-interfaces:interfaces').fetch('interface')
        if field == 'prefix'
          management = interfaces.find { |entry| entry.fetch('name') == 'enmgmt0' }
          management.fetch('ietf-ip:ipv6').fetch('address').first['prefix-length'] = 63
        else
          transit = interfaces.find { |entry| entry.fetch('name') == 'enmwanbr0' }
          transit.fetch('ietf-ip:ipv6').fetch('goodkind-mwan-steering:route').first['metric'] = 42
        end
        File.write(current, JSON.generate(document))
        expect do
          activation_play(directory, 'activate', prior, current, prepared: true, corrupt: false,
                                                                 domain: 'home.goodkind.io')
        end.to raise_error(RuntimeError, /prior role inputs and applied state passed recovery verification/)
        expect(role_state('enmgmt0').fetch('NetworkFile')).to eq('/etc/systemd/network/10-mgmt.network')
        expect(JSON.parse(guest('cat', '/etc/mwan/network.json'))).to eq(JSON.parse(File.read(prior)))
      end
    end
  end

  it 'reports the original rejection and failed recovery while retaining the backup' do
    with_real_network do |directory, prior, current|
      expect do
        activation_play(directory, 'recovery_failure', prior, current, prepared: true, corrupt: false,
                                                                       domain: 'home.goodkind.io')
      end.to raise_error(RuntimeError) { |error|
        expect(error.message).to match(/Role activation failed: Reject the deliberately invalid input/)
        expect(error.message).to match(/Recovery failed: Restore prior role inputs within the guest/)
        backup_path = error.message.match(%r{The role input backup remains at (/var/lib/mwan/role-inputs-\w+)})[1]
        expect(JSON.parse(guest('cat', "#{backup_path}/network.json"))).to eq(JSON.parse(File.read(prior)))
      }
    end
  end

  def remove_initial_role_configuration
    guest('systemctl', 'disable', '--now', 'mwan-ifmgr@wan')
    guest('rm', '-f', '/etc/mwan/config.toml', '/etc/mwan/network.json',
          '/etc/systemd/network/10-mgmt.network', '/etc/systemd/network/40-mwanbr.network',
          '/etc/systemd/network/20-enmgmt0.network', '/etc/systemd/network/20-enmwanbr0.network',
          '/etc/systemd/network/10-mgmt.link', '/etc/systemd/network/40-mwanbr.link',
          '/etc/systemd/network/20-enmgmt0.link', '/etc/systemd/network/20-enmwanbr0.link')
    %w[enmgmt0 enmwanbr0].each_with_index do |name, index|
      guest('ip', 'link', 'set', name, 'down')
      guest('ip', 'link', 'set', name, 'name', "freshrole#{index}")
    end
  end

  it 'configures a first install through the original naming and role templates' do
    with_real_network do |directory, prior, current|
      remove_initial_role_configuration
      activation_play(directory, 'activate', prior, current, prepared: true, corrupt: false,
                                                             domain: 'home.goodkind.io')
      expect(role_state('enmgmt0').fetch('NetworkFile')).to eq('/etc/systemd/network/20-enmgmt0.network')
      expect(role_state('enmwanbr0').fetch('NetworkFile')).to eq('/etc/systemd/network/20-enmwanbr0.network')
      expect(guest('systemctl', 'show', 'mwan-ifmgr@wan', '--property=ActiveState', '--value').strip).to eq('active')
    end
  end

  it 'restores absent configuration and stops the new writer after first-install staging fails' do
    with_real_network do |directory, prior, current|
      remove_initial_role_configuration
      expect do
        activation_play(directory, 'stage_failure', prior, current, prepared: true, corrupt: false,
                                                                    domain: 'home.goodkind.io')
      end.to raise_error(RuntimeError, /prior role inputs and applied state passed recovery verification/)
      expect(guest('find', '/etc/mwan', '-name', 'config.toml', '-o', '-name', 'network.json')).to eq('')
      expect(role_state('enmgmt0').fetch('NetworkFile')).to eq('/etc/systemd/network/10-mgmt.network')
      expect(role_state('enmwanbr0').fetch('NetworkFile')).to eq('/etc/systemd/network/40-mwanbr.network')
      expect(guest('systemctl', 'show', 'mwan-ifmgr@wan', '--property=ActiveState', '--value').strip).to eq('inactive')
      expect(guest('systemctl', 'show', 'mwan-ifmgr@wan', '--property=UnitFileState', '--value').strip).to eq('disabled')
    end
  end

  def copy_to_guest(source, destination)
    command = ['docker', 'cp', source, "#{@container}:#{destination}"]
    if @physical_port
      command = ['scp', '-i', @physical_key, '-P', @physical_port, '-o', 'BatchMode=yes',
                 '-o', 'IdentitiesOnly=yes', '-o', "UserKnownHostsFile=#{@physical_known_hosts}",
                 source, "probe@127.0.0.1:/tmp/#{File.basename(source)}"]
    end
    result = CommandRunner.run({}, command,
                               chdir: AnsibleRender::REPOSITORY_ROOT, timeout_seconds: 30)
    raise result.output unless result.exit_status.success? && !result.timed_out

    guest('install', '-m', '0600', "/tmp/#{File.basename(source)}", destination) if @physical_port
  end

  def physical_server(*arguments)
    port = ENV.fetch('MWAN_ROLE_ACTIVATION_TEST_SERVER_PORT')
    command = ['ssh', '-i', @physical_key, '-p', port, '-o', 'BatchMode=yes', '-o', 'IdentitiesOnly=yes',
               '-o', "UserKnownHostsFile=#{@physical_known_hosts}", 'probe@127.0.0.1',
               "sudo -- #{Shellwords.join(arguments)}"]
    result = CommandRunner.run({}, command, chdir: AnsibleRender::REPOSITORY_ROOT, timeout_seconds: 30)
    raise result.output unless result.exit_status.success? && !result.timed_out

    result.output
  end

  def rename_physical_link(hardware_address, name, server: false)
    links = JSON.parse(server ? physical_server('ip', '-json', 'link', 'show') : guest('ip', '-json', 'link', 'show'))
    current = links.find { |entry| entry.fetch('address') == hardware_address }.fetch('ifname')
    command = :guest
    command = :physical_server if server
    expect(send(command, 'ethtool', '-P', current).strip).to eq("Permanent address: #{hardware_address}")
    unless current == name
      send(command, 'ip', 'link', 'set', current, 'down')
      send(command, 'ip', 'link', 'set', current, 'name', name)
    end
    send(command, 'ip', 'link', 'set', name, 'up')
  end

  def create_transfer_links
    guest('systemctl', 'stop', 'mwan-ifmgr@wan')
    namespaces = guest('ip', 'netns', 'list').lines.map { |line| line.split.first }
    guest('ip', 'netns', 'delete', 'transfer-downstream') if namespaces.include?('transfer-downstream')
    namespaces = physical_server('ip', 'netns', 'list').lines.map { |line| line.split.first }
    %w[transfer-webpass transfer-backup].each do |namespace|
      physical_server('ip', 'netns', 'delete', namespace) if namespaces.include?(namespace)
    end
    links = JSON.parse(guest('ip', '-json', 'link', 'show')).map { |entry| entry.fetch('ifname') }
    guest('ip', 'link', 'delete', 'enmwanbr0') if links.include?('enmwanbr0')
    [
      ['enwebpass0', 'transfer-webpass', 'bc:24:11:be:8e:b4', '10.241.204.1/29', 'fd39:10::1/64'],
      ['enrouted0', 'transfer-backup', 'bc:24:11:a5:70:08', '10.240.208.1/24', '3d06:bad:b01:260::1/64']
    ].each do |name, namespace, hardware_address, ipv4, ipv6|
      create_physical_provider(name, namespace, hardware_address, ipv4, ipv6)
    end
    create_transfer_downstream
    guest('ip', 'netns', 'exec', 'transfer-downstream', 'ip', 'route', 'add', 'default', 'via', '10.240.240.3')
    guest('ip', 'netns', 'exec', 'transfer-downstream', 'ip', '-6', 'route', 'add', 'default', 'via', '3d06:bad:b01:201::3')
    [['transfer-webpass', '10.241.204.2', 'fd39:10::2'],
     ['transfer-backup', '10.240.208.2', '3d06:bad:b01:260::2']].each do |namespace, ipv4, ipv6|
      physical_server('ip', 'netns', 'exec', namespace, 'ip', 'address', 'add', '198.18.2.1/32', 'dev', 'lo')
      physical_server('ip', 'netns', 'exec', namespace, 'ip', '-6', 'address', 'add', 'fd39:99::1/128', 'dev', 'lo')
      physical_server('ip', 'netns', 'exec', namespace, 'ip', 'route', 'add', '10.240.240.0/29', 'via', ipv4)
      physical_server('ip', 'netns', 'exec', namespace, 'ip', '-6', 'route', 'add', '3d06:bad:b01:201::/64', 'via', ipv6)
    end
  end

  def create_physical_provider(name, namespace, hardware_address, ipv4, ipv6)
    rename_physical_link(hardware_address, name)
    server_mac = '52:54:00:52:29:01'
    server_mac = '52:54:00:52:29:02' if name == 'enrouted0'
    rename_physical_link(server_mac, name, server: true)
    physical_server('ip', 'netns', 'add', namespace)
    physical_server('ip', 'link', 'set', name, 'netns', namespace)
    physical_server('ip', 'netns', 'exec', namespace, 'ip', 'link', 'set', 'lo', 'up')
    physical_server('ip', 'netns', 'exec', namespace, 'ip', 'link', 'set', name, 'name', 'transferpeer')
    physical_server('ip', 'netns', 'exec', namespace, 'ip', 'link', 'set', 'transferpeer', 'up')
    physical_server('ip', 'netns', 'exec', namespace, 'ip', 'address', 'add', ipv4, 'dev', 'transferpeer')
    physical_server('ip', 'netns', 'exec', namespace, 'ip', '-6', 'address', 'add', ipv6, 'dev', 'transferpeer')
  end

  def create_transfer_downstream
    guest('ip', 'netns', 'add', 'transfer-downstream')
    guest('ip', 'link', 'add', 'enmwanbr0', 'type', 'veth', 'peer', 'name', 'transferpeer')
    guest('ip', 'link', 'set', 'transferpeer', 'netns', 'transfer-downstream')
    guest('ip', 'link', 'set', 'enmwanbr0', 'address', 'bc:24:11:49:5d:94', 'up')
    guest('ip', 'netns', 'exec', 'transfer-downstream', 'ip', 'link', 'set', 'lo', 'up')
    guest('ip', 'netns', 'exec', 'transfer-downstream', 'ip', 'link', 'set', 'transferpeer', 'up')
    guest('ip', 'netns', 'exec', 'transfer-downstream', 'ip', 'address', 'add', '10.240.240.2/29', 'dev', 'transferpeer')
    guest('ip', 'netns', 'exec', 'transfer-downstream', 'ip', 'address', 'add', '10.240.240.4/29', 'dev', 'transferpeer')
    guest('ip', 'netns', 'exec', 'transfer-downstream', 'ip', '-6', 'address', 'add', '3d06:bad:b01:201::2/64', 'dev', 'transferpeer')
  end

  def transfer_documents(prior, current)
    document = JSON.parse(File.read(prior))
    document.fetch('ietf-interfaces:interfaces').fetch('interface').each do |entry|
      next unless %w[webpass routed].include?(entry.fetch('goodkind-mwan-steering:connection-id'))

      health = entry.fetch('goodkind-mwan-steering:wan').fetch('health')
      health['targets-v4'] = [entry.fetch('ietf-ip:ipv4').fetch('goodkind-mwan-steering:gateway')]
      health['targets-v6'] = []
      health['http-urls'] = []
      health['success-threshold'] = 1
      next unless entry.fetch('goodkind-mwan-steering:connection-id') == 'webpass'

      entry['ietf-ip:ipv6'] = {
        'address' => [{ 'ip' => 'fd39:10::2', 'prefix-length' => 64 }], 'forwarding' => true,
        'goodkind-mwan-steering:accept-ra' => false, 'goodkind-mwan-steering:dhcp' => false,
        'goodkind-mwan-steering:gateway' => 'fd39:10::1',
        'goodkind-mwan-steering:translation' => { 'mode' => 'native' }
      }
    end
    File.write(prior, JSON.generate(document))
    selected = document.fetch('ietf-interfaces:interfaces').fetch('interface').find do |entry|
      entry.fetch('goodkind-mwan-steering:connection-id') == 'webpass'
    end
    selected['goodkind-mwan-steering:owner'] = 'mwan'
    selected.delete('goodkind-mwan-steering:link-files')
    selected.fetch('ietf-ip:ipv4').fetch('goodkind-mwan-steering:translation').fetch('static-mapping').each do |mapping|
      mapping['delivery'] = 'local'
    end
    File.write(current, JSON.generate(document))
  end

  def transfer_packet_checks
    %w[excluded acquired recovered selected].flat_map do |phase|
      checks = [['ipv4', 'routed', 'unaffected', 'transfer-backup', '10.240.208.1', '10.240.240.4'],
                ['ipv6', 'routed', 'unaffected', 'transfer-backup', '3d06:bad:b01:260::1', '3d06:bad:b01:201::2']]
      unless phase == 'excluded'
        checks += [['ipv4', 'webpass', 'selected', 'transfer-webpass', '10.241.204.1', '10.241.204.4'],
                   ['ipv6', 'webpass', 'selected', 'transfer-webpass', 'fd39:10::1', '3d06:bad:b01:201::2']]
      end
      checks.map do |check|
        family, connection, coverage, namespace, source, destination = check
        { 'phase' => phase, 'host' => 'physical_server', 'family' => family,
          'connection_id' => connection, 'coverage' => coverage,
          'description' => "#{phase} #{connection} #{family} inbound request and downstream reply",
          'argv' => ['ip', 'netns', 'exec', namespace, 'ping', '-I', source,
                     '-c', '1', '-W', '2', destination] }
      end
    end
  end

  def expect_static_provider_baseline
    addresses = role_state('enwebpass0').fetch('Addresses')
    expect(addresses).to include(a_hash_including('Family' => 2, 'Address' => [10, 241, 204, 2],
                                                  'PrefixLength' => 29, 'ConfigSource' => 'static'))
    expect(addresses).to include(a_hash_including('Family' => 10,
                                                  'Address' => [253, 57, 0, 16, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2],
                                                  'PrefixLength' => 64, 'ConfigSource' => 'static'))
  end

  it 'transfers a complete provider through external ownership and reverses with downstream replies' do
    skip 'A real physical fixture with permanent MAC addresses is required' unless @physical_port

    with_real_network(provider_transfer: true) do |directory, prior, current|
      create_transfer_links
      activation_play(directory, 'prepare', prior, current, prepared: true, corrupt: false,
                                                            domain: 'home.goodkind.io', publish: true)
      expect(JSON.parse(guest('ip', '-json', 'address', 'show', 'dev', 'enwebpass0')).first.fetch('addr_info'))
        .to include(a_hash_including('local' => '10.241.204.3', 'prefixlen' => 32))
      expect_static_provider_baseline
      packet_checks = transfer_packet_checks
      activation_play(directory, 'activate', prior, current, prepared: true, corrupt: false,
                                                             domain: 'home.goodkind.io', publish: true,
                                                             packet_checks: packet_checks)
      expect(role_state('enwebpass0').fetch('AdministrativeState')).to eq('unmanaged')
      journal = JSON.parse(guest('cat', '/var/lib/mwan/test-addresses.json'))
      expect(journal.fetch('boot_id')).to eq(guest('cat', '/proc/sys/kernel/random/boot_id').strip)
      recorded = journal.fetch('objects').select do |entry|
        entry.fetch('connection_id') == 'webpass' && !entry.key?('scope') && entry.key?('prefix')
      end
      prefixes = recorded.map { |entry| entry.fetch('prefix') }
      expect(prefixes).to include('10.241.204.2/29', '10.241.204.3/32', '10.241.204.4/32',
                                  '10.241.204.5/32', '10.241.204.6/32', 'fd39:10::2/64')
      expect(JSON.parse(guest('cat', '/etc/mwan/network.json'))).to eq(JSON.parse(File.read(current)))
      activation_play(directory, 'activate', current, prior, prepared: true, corrupt: false,
                                                             domain: 'home.goodkind.io', publish: true,
                                                             packet_checks: packet_checks)
      expect(role_state('enwebpass0').fetch('NetworkFile')).to eq('/etc/systemd/network/20-enwebpass0.network')
      expect(JSON.parse(guest('cat', '/etc/mwan/network.json'))).to eq(JSON.parse(File.read(prior)))
    end
  end

  %w[partial malformed].each do |failure|
    it "rejects a #{failure} prior runtime pair before staging" do
      with_real_network do |directory, prior, current|
        before = %w[enmgmt0 enmwanbr0].map { |name| role_state(name).fetch('NetworkFile') }
        legacy = guest('sha256sum', '/etc/systemd/network/10-mgmt.network',
                       '/etc/systemd/network/40-mwanbr.network')
        if failure == 'partial'
          guest('rm', '/etc/mwan/config.toml')
          expected = /Both runtime configuration files must exist or both must be absent/
        else
          guest('truncate', '-s', '0', '/etc/mwan/network.json')
          expected = /Validate the prior network document before staging/
        end
        rejected_network = guest('cat', '/etc/mwan/network.json')
        expect do
          activation_play(directory, 'activate', prior, current, prepared: true, corrupt: false,
                                                                 domain: 'home.goodkind.io')
        end.to raise_error(RuntimeError, expected)
        expect(guest('cat', '/etc/mwan/network.json')).to eq(rejected_network)
        expect(guest('sha256sum', '/etc/systemd/network/10-mgmt.network',
                     '/etc/systemd/network/40-mwanbr.network')).to eq(legacy)
        expect(%w[enmgmt0 enmwanbr0].map { |name| role_state(name).fetch('NetworkFile') }).to eq(before)
      end
    end
  end
end
