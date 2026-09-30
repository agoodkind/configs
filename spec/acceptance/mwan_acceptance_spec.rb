# frozen_string_literal: true

require 'digest'
require 'fileutils'
require 'json'
require 'open3'
require 'securerandom'
require 'socket'
require 'tmpdir'
require_relative '../support/command_runner'
require_relative '../../lib/mwan_acceptance'

# The SSH fixture starts an isolated server with temporary host and client keys.
module MwanAcceptanceSSH
  def ssh
    command('ssh-keygen', '-q', '-t', 'ed25519', '-N', '', '-f', File.join(root, 'identity'))
    command('ssh-keygen', '-q', '-t', 'ed25519', '-N', '', '-f', File.join(root, 'host-key'))
    port = TCPServer.open('127.0.0.1', 0) { |server| server.addr[1] }
    File.write(File.join(root, 'authorized_keys'), File.read(File.join(root, 'identity.pub')))
    write_sshd_config(port)
    FileUtils.mkdir_p('/run/sshd')
    unit('ssh', ['/usr/sbin/sshd', '-D', '-e', '-f', File.join(root, 'sshd.conf')])
    public_key = File.read(File.join(root, 'host-key.pub')).split.first(2).join(' ')
    File.write(File.join(root, 'known_hosts'), "[127.0.0.1]:#{port} #{public_key}\n")
    File.write(File.join(root, 'ssh_config'),
               "Host fixture\n HostName 127.0.0.1\n User root\n Port #{port}\n IdentityFile #{root}/identity\n UserKnownHostsFile #{root}/known_hosts\n StrictHostKeyChecking yes\n")
    wait_port(port)
    command('ssh', '-F', File.join(root, 'ssh_config'), 'fixture', 'hostname')
  end

  def write_sshd_config(port)
    configuration = <<~CONFIG
      Port #{port}
      ListenAddress 127.0.0.1
      HostKey #{root}/host-key
      AuthorizedKeysFile #{root}/authorized_keys
      PermitRootLogin yes
      StrictModes no
      PasswordAuthentication no
      UsePAM no
      PidFile #{root}/sshd.pid
    CONFIG
    File.write(File.join(root, 'sshd.conf'), configuration)
  end

  def wait_port(port)
    deadline = Process.clock_gettime(Process::CLOCK_MONOTONIC) + 5
    loop do
      TCPSocket.open('127.0.0.1', port, &:close)
      return
    rescue Errno::ECONNREFUSED
      raise if Process.clock_gettime(Process::CLOCK_MONOTONIC) >= deadline

      sleep 0.05
    end
  end
end

# The fixture uses a dedicated privileged Linux host with real systemd and SSH.
class MwanAcceptanceFixture
  include MwanAcceptanceSSH

  attr_reader :root, :namespaces, :plan, :shm_prefix

  def initialize
    @root = Dir.mktmpdir('mwan-acceptance-')
    @namespaces = %w[gateway router client-a client-b simulator].to_h { |name| [name, "ma-#{SecureRandom.hex(3)}"] }
    @units = []
    @plan = nil
    @shm_prefix = "ma_acceptance_#{SecureRandom.hex(8)}_"
  end

  def command(*arguments)
    result = CommandRunner.capture(arguments, stdin_data: '', chdir: root, timeout_seconds: 20)
    raise "#{arguments.inspect}: deadline expired" if result.timed_out
    raise "#{arguments.inspect}: #{result.error_output}" unless result.exit_status.success?

    result.output
  end

  def setup
    namespaces.each_value do |name|
      command('ip', 'netns', 'add', name)
      command('ip', '-n', name, 'link', 'set', 'lo', 'up')
    end
    downstream
    upstream
    ssh
  end

  def downstream
    command('ip', '-n', namespaces.fetch('router'), 'link', 'add', 'lan0', 'type', 'bridge')
    command('ip', '-n', namespaces.fetch('router'), 'link', 'set', 'lan0', 'up')
    address('router', 'lan0', '10.251.0.1/24', 'fd51::1/64')
    %w[client-a client-b].each_with_index { |name, index| client(name, index + 2) }
    pair('gateway', 'enmwanbr0', 'router', 'transit0')
    address('gateway', 'enmwanbr0', '10.251.254.1/29', 'fd51:fe::1/64')
    address('router', 'transit0', '10.251.254.2/29', 'fd51:fe::2/64')
    command('ip', '-n', namespaces.fetch('router'), 'route', 'add', 'default', 'via', '10.251.254.1')
    command('ip', '-n', namespaces.fetch('router'), '-6', 'route', 'add', 'default', 'via', 'fd51:fe::1')
    command('ip', '-n', namespaces.fetch('gateway'), '-6', 'route', 'add', 'fd51::/64', 'via', 'fd51:fe::2')
    command('ip', 'netns', 'exec', namespaces.fetch('router'), 'sysctl', '-w', 'net.ipv4.ip_forward=1', 'net.ipv6.conf.all.forwarding=1')
    command('ip', 'netns', 'exec', namespaces.fetch('router'), 'nft', 'add', 'table', 'ip', 'fixture_nat')
    command('ip', 'netns', 'exec', namespaces.fetch('router'), 'nft', 'add', 'chain', 'ip', 'fixture_nat', 'postrouting', '{ type nat hook postrouting priority srcnat; }')
    command('ip', 'netns', 'exec', namespaces.fetch('router'), 'nft', 'add', 'rule', 'ip', 'fixture_nat', 'postrouting', 'oifname', 'transit0', 'ip', 'saddr', '10.251.0.0/24', 'snat', 'to',
            '10.251.254.2')
  end

  def upstream
    pair('gateway', 'enprovider0', 'simulator', 'eth0')
    address('gateway', 'enprovider0', '10.251.1.1/24', 'fd51:1::1/64')
    address('simulator', 'eth0', '10.251.1.2/24', 'fd51:1::2/64')
    address('simulator', 'lo', '198.51.100.80/32', 'fd52::80/128')
    command('ip', '-n', namespaces.fetch('simulator'), 'route', 'add', '10.251.254.0/29', 'via', '10.251.1.1')
    command('ip', '-n', namespaces.fetch('simulator'), '-6', 'route', 'add', 'fd51::/64', 'via', 'fd51:1::1')
    command('ip', '-n', namespaces.fetch('simulator'), '-6', 'route', 'add', 'fd53::/64', 'via', 'fd51:1::1')
    command('ip', '-n', namespaces.fetch('gateway'), 'route', 'add', 'default', 'via', '10.251.1.2')
    command('ip', '-n', namespaces.fetch('gateway'), '-6', 'route', 'add', 'default', 'via', 'fd51:1::2')
    command('ip', 'netns', 'exec', namespaces.fetch('gateway'), 'sysctl', '-w', 'net.ipv4.ip_forward=1', 'net.ipv6.conf.all.forwarding=1')
  end

  def client(name, number)
    pair('router', "port#{number}", name, 'eth0')
    command('ip', '-n', namespaces.fetch('router'), 'link', 'set', "port#{number}", 'master', 'lan0')
    command('ip', '-n', namespaces.fetch(name), 'link', 'set', 'eth0', 'address', format('02:00:00:00:00:%<number>02x', number: number))
    address(name, 'eth0', "10.251.0.#{number}/24", "fd51::#{number}/64")
    command('ip', '-n', namespaces.fetch(name), 'route', 'add', 'default', 'via', '10.251.0.1')
    command('ip', '-n', namespaces.fetch(name), '-6', 'route', 'add', 'default', 'via', 'fd51::1')
  end

  def pair(left, left_interface, right, right_interface)
    temporary = "mat#{SecureRandom.hex(3)}"
    command('ip', '-n', namespaces.fetch(left), 'link', 'add', left_interface, 'type', 'veth', 'peer', 'name', temporary)
    command('ip', '-n', namespaces.fetch(left), 'link', 'set', temporary, 'netns', namespaces.fetch(right))
    command('ip', '-n', namespaces.fetch(right), 'link', 'set', temporary, 'name', right_interface)
    [[left, left_interface], [right, right_interface]].each { |name, interface| command('ip', '-n', namespaces.fetch(name), 'link', 'set', interface, 'up') }
  end

  def address(name, interface, *addresses)
    addresses.each { |value| command('ip', '-n', namespaces.fetch(name), 'address', 'add', value, 'dev', interface, 'nodad') }
  end

  def unit(label, arguments)
    name = "ma-fixture-#{label}-#{SecureRandom.hex(3)}"
    @units.push(name)
    command('systemd-run', '--quiet', "--unit=#{name}", '--property=RuntimeMaxSec=300', '--', *arguments)
    name
  end

  def teardown
    errors = []
    @units.reverse_each do |name|
      cleanup(errors) do
        state = command('systemctl', 'show', name, '--property=ActiveState', '--value').strip
        command('systemctl', 'stop', name) unless state == 'inactive'
      end
    end
    namespaces.each_value { |name| cleanup(errors) { command('ip', 'netns', 'delete', name) } }
    Dir.glob("/dev/shm/#{shm_prefix}*").each { |path| cleanup(errors) { File.unlink(path) } }
    FileUtils.remove_entry(root)
    raise errors.join("\n") unless errors.empty?
  end

  def cleanup(errors)
    yield
  rescue StandardError => e
    errors.push(e.message)
  end
end

# The routing fixture uses the production BGP installer and a real router peer.
class MwanAcceptanceBGPFixture < MwanAcceptanceFixture
  def bgp_config
    <<~TOML
      [agent]
      vsock_port = 0
      tcp_addr = "127.0.0.1:0"
      deploy_expected = false
      log_file = #{File.join(root, 'agent.jsonl').inspect}
      [bgp]
      enabled = true
      use_wanconfig = true
      asn = 64512
      router_id = "10.251.254.1"
      next_hop_v6 = "fd51:fe::1"
      listen_port = 179
      learned_route_iface = "enmwanbr0"
      dynamic_neighbors = ["fd51:fe::/64"]
      keepalive_seconds = 1
      hold_seconds = 3
      [bgp.forwarding_readiness]
      socket_path = #{File.join(root, 'forwarding.sock').inspect}
      poll_interval_milliseconds = 100
      read_timeout_milliseconds = 100
    TOML
  end

  def bgp
    unit('agent', ['ip', 'netns', 'exec', namespaces.fetch('gateway'), 'env', "MWAN_CONFIG=#{root}/config.toml", binary, 'agent'])
    path = File.join(root, 'bird.conf')
    File.write(path, <<~BIRD)
      router id 10.251.254.2;
      log stderr all;
      protocol device {}
      protocol static lan { ipv6; route fd51::/64 blackhole; }
      protocol bgp upstream {
        local fd51:fe::2 as 64512;
        neighbor fd51:fe::1 as 64512;
        connect retry time 1;
        ipv6 { import none; export all; next hop self; };
      }
    BIRD
    unit('bird', ['ip', 'netns', 'exec', namespaces.fetch('router'), '/usr/sbin/bird', '-f', '-c', path, '-s', File.join(root, 'bird.ctl')])
  end

  def wait_learned_route
    deadline = Process.clock_gettime(Process::CLOCK_MONOTONIC) + 20
    loop do
      routes = JSON.parse(command('ip', '-n', namespaces.fetch('gateway'), '-6', '-j', 'route', 'show', 'table', '201'))
      return if routes.any? { |route| route['dst'] == 'fd51::/64' && route['gateway'] == 'fd51:fe::2' && route['protocol'] == 'bgp' }
      raise 'The real BGP peer did not install its downstream route in provider table201' if Process.clock_gettime(Process::CLOCK_MONOTONIC) >= deadline

      sleep 0.1
    end
  end
end

# The destination updater uses the production service in the gateway namespace.
module MwanAcceptanceUpdater
  def setup_updater
    service_path = '/etc/systemd/system/mwan-update-att-pinned-dests.service'
    script_path = '/usr/local/bin/update-att-pinned-dests.sh'
    environment_path = '/etc/mwan/mwan.env'
    drop_in = "#{service_path}.d"
    paths = [service_path, script_path, environment_path, drop_in]
    raise 'The private fixture updater paths must be absent' if paths.any? { |path| File.exist?(path) }

    @updater_paths = paths
    sources = File.expand_path('../../mwan', __dir__)
    FileUtils.cp(File.join(sources, 'services/mwan-update-att-pinned-dests.service'), service_path)
    FileUtils.cp(File.join(sources, 'scripts/update-att-pinned-dests.sh'), script_path)
    FileUtils.chmod(0o755, script_path)
    write_updater_environment(environment_path)
    FileUtils.mkdir_p(drop_in)
    write_updater_unit(File.join(drop_in, 'fixture.conf'))
    command('systemctl', 'daemon-reload')
  end

  def write_updater_environment(path)
    File.write(path, <<~ENVIRONMENT)
      MWAN_PINNED_SET_V4_NAME=fixture_pinned_v4
      MWAN_PINNED_SET_V6_NAME=fixture_pinned_v6
      MWAN_ATT_PINNED_V4_SEED_CIDRS=192.0.2.0/24
      MWAN_ATT_PINNED_V6_SEED_CIDRS=2001:db8:100::/48
    ENVIRONMENT
    FileUtils.chmod(0o600, path)
  end

  def write_updater_unit(path)
    File.write(path, <<~UNIT)
      [Unit]
      Wants=
      Wants=network-online.target #{service}.service
      After=
      After=network-online.target #{service}.service
      [Service]
      NetworkNamespacePath=/run/netns/#{namespaces.fetch('gateway')}
    UNIT
  end

  def wait_updater
    deadline = Process.clock_gettime(Process::CLOCK_MONOTONIC) + 20
    loop do
      status = command('systemctl', 'show', 'mwan-update-att-pinned-dests.service', '--property=Result', '--property=ActiveState')
      sets = command('ip', 'netns', 'exec', namespaces.fetch('gateway'), 'nft', 'list', 'table', 'inet', 'mangle')
      return if status.include?("Result=success\n") && status.include?("ActiveState=inactive\n") &&
                sets.include?('192.0.2.0/24') && sets.include?('2001:db8:100::/48')
      raise command('journalctl', '-u', 'mwan-update-att-pinned-dests.service', '--no-pager', '-n', '30') if Process.clock_gettime(Process::CLOCK_MONOTONIC) >= deadline

      sleep 0.1
    end
  end

  def teardown
    errors = []
    if @updater_paths
      cleanup(errors) { command('systemctl', 'stop', 'mwan-update-att-pinned-dests.service') }
      @updater_paths.each { |path| cleanup(errors) { FileUtils.remove_entry(path) } }
      cleanup(errors) { command('systemctl', 'daemon-reload') }
    end
    super
    raise errors.join("\n") unless errors.empty?
  end
end

# This fixture runs the production daemon against real namespace routes.
class MwanAcceptanceDaemonFixture < MwanAcceptanceBGPFixture
  include MwanAcceptanceUpdater

  attr_reader :service, :binary

  def setup
    super
    @binary = ENV.fetch('MWAN_ACCEPTANCE_BINARY')
    command(binary, 'install', '--role', 'wan', '--root', File.join(root, 'installation'), '--apply')
    FileUtils.mkdir_p('/etc/mwan')
    FileUtils.mkdir_p('/usr/local/share/wanconfig')
    FileUtils.cp_r(File.join(root, 'installation/usr/local/share/wanconfig/yang'), '/usr/local/share/wanconfig/')
    File.write('/etc/mwan/network.json', JSON.pretty_generate(network))
    write_daemon_config
    @service = unit('daemon', ['ip', 'netns', 'exec', namespaces.fetch('gateway'), 'env', "MWAN_CONFIG=#{root}/config.toml",
                               "SYSREPO_REPOSITORY_PATH=#{root}/installation/etc/sysrepo", "SYSREPO_SHM_PREFIX=#{shm_prefix}", binary, 'ifmgr', '--role', 'wan'])
    setup_updater
    server
    bgp
    wait_ready
    wait_updater
    wait_learned_route
  end

  def write_daemon_config
    configuration = <<~CONFIG
      [ifmgr]
      role = "wan"
      reconcile_interval = "100ms"
      json_log_file = #{File.join(root, 'ifmgr.jsonl').inspect}
      [ifmgr.iface.enmwanbr0]
      [ifmgr.modules.addresses]
      state_file = #{File.join(root, 'owned-addresses.json').inspect}
      [ifmgr.modules.health]
      state_file = #{File.join(root, 'health.json').inspect}
      [ifmgr.modules.wan.routes]
      health_state_file = #{File.join(root, 'health.json').inspect}
      [wanconfig]
      publish = true
    CONFIG
    File.write(File.join(root, 'config.toml'), configuration + bgp_config)
  end

  def network
    provider = { 'name' => 'enprovider0', 'type' => 'iana-if-type:other', 'goodkind-mwan-steering:owner' => 'networkd',
                 'goodkind-mwan-steering:link-files' => 'hand-authored',
                 'ietf-ip:ipv4' => { 'goodkind-mwan-steering:translation' => { 'mode' => 'ietf-nat:napt44',
                                                                               'static-mapping' => [{ 'external' => '10.251.1.5', 'internal' => '10.251.254.2' }] } },
                 'ietf-ip:ipv6' => { 'goodkind-mwan-steering:translation' => { 'mode' => 'ietf-nat:nptv6',
                                                                               'nptv6' => { 'internal-prefix' => 'fd51::/64', 'external-source' => 'configured', 'external-prefix' => 'fd53::/64' } } },
                 'goodkind-mwan-steering:wan' => { 'name' => 'provider', 'table-id' => 201, 'fw-mark' => 1, 'fw-mark-prio' => 201, 'from-prio' => 51, 'health' => health },
                 'goodkind-mwan-steering:steering' => { 'tier' => 0, 'weight' => 1 } }
    { 'ietf-interfaces:interfaces' => { 'interface' => [provider, { 'name' => 'enmwanbr0', 'type' => 'iana-if-type:other' }, { 'name' => 'enmgmt0', 'type' => 'iana-if-type:other' }],
                                        'goodkind-mwan-steering:steering-group' => { 'hash-mode' => 'random', 'reserved-tables' => [],
                                                                                     'routes' => { 'internal-iface' => 'enmwanbr0', 'internal-net-v4' => '10.251.254.0/29' },
                                                                                     'translation' => { 'internal-prefix' => 'fd51::/64', 'opnsense-edge-v6' => 'fd51:fe::2',
                                                                                                        'mwanbr-edge-v6' => 'fd51:fe::1' },
                                                                                     'firewall' => { 'management-interface' => 'enmgmt0',
                                                                                                     'management-service' => [{ 'protocol' => 'tcp', 'port' => 22 }],
                                                                                                     'pinned-provider' => 'provider', 'pinned-source-v4' => '10.251.254.2',
                                                                                                     'pinned-source-port' => 51_820, 'pinned-destination-port' => 51_821,
                                                                                                     'pinned-set-v4-name' => 'fixture_pinned_v4', 'pinned-set-v6-name' => 'fixture_pinned_v6' },
                                                                                     'health' => { 'probe-timeout' => 500 } } } }
  end

  def health
    { 'enabled' => true, 'ping-count' => 1, 'success-threshold' => 1, 'failure-threshold' => 1, 'recovery-threshold' => 1,
      'check-interval' => 1, 'targets-v4' => ['10.251.1.2'], 'targets-v6' => ['fd51:1::2'], 'http-urls' => [] }
  end

  def server
    path = File.join(root, 'server.rb')
    File.write(path, <<~RUBY)
      require 'socket'
      server = TCPServer.new('::', 18080)
      loop do
        socket = server.accept
        request = socket.gets
        socket.gets until $_ == "\\r\\n"
        status = request.include?('/failure ') ? 503 : 200
        body = "mwan-acceptance\\n"
        socket.write("HTTP/1.1 \#{status} Result\\r\\nContent-Length: \#{body.bytesize}\\r\\nConnection: close\\r\\n\\r\\n\#{body}")
        socket.close
      end
    RUBY
    %w[simulator router].each { |name| unit("http-#{name}", ['ip', 'netns', 'exec', namespaces.fetch(name), RbConfig.ruby, path]) }
  end

  def operational
    JSON.parse(command('env', "SYSREPO_REPOSITORY_PATH=#{root}/installation/etc/sysrepo", "SYSREPO_SHM_PREFIX=#{shm_prefix}",
                       'sysrepocfg', '-X', '-d', 'operational', '-m', 'ietf-interfaces', '-f', 'json'))
  end

  def wait_ready
    deadline = Process.clock_gettime(Process::CLOCK_MONOTONIC) + 20
    loop do
      member = operational.fetch('ietf-interfaces:interfaces', {}).fetch('interface', []).find { |entry| entry['name'] == 'enprovider0' }
      ready = member && member.dig('goodkind-mwan-steering:steering', 'state', 'health') == 'healthy'
      return if ready && member.dig('ietf-ip:ipv4', 'goodkind-mwan-steering:ownership-family-state', 'routing') == 'ready' &&
                member.dig('ietf-ip:ipv6', 'goodkind-mwan-steering:ownership-family-state', 'routing') == 'ready'

      raise command('journalctl', '-u', service, '--no-pager', '-n', '30') if Process.clock_gettime(Process::CLOCK_MONOTONIC) >= deadline

      sleep 0.1
    end
  end
end

# The declared plan uses actual fixture identities and capture calibration.
module MwanAcceptancePlanInputs
  def targets
    namespaces.map do |id, namespace|
      { 'id' => id, 'host' => 'fixture', 'adapter' => 'linux', 'vmid' => nil, 'namespace' => namespace,
        'hostname' => command('hostname').strip, 'machine_id' => File.read('/etc/machine-id').strip, 'pve_hostname' => nil }
    end
  end

  def clients
    router = JSON.parse(command('ip', '-n', namespaces.fetch('router'), '-j', 'link', 'show', 'lan0')).first.fetch('address')
    %w[client-a client-b].each_with_index.map do |name, index|
      number = index + 2
      { 'target' => name, 'interface' => 'eth0', 'mac' => format('02:00:00:00:00:%<number>02x', number: number),
        'addresses' => { '4' => "10.251.0.#{number}", '6' => "fd51::#{number}" },
        'gateways' => { '4' => ['10.251.0.1'], '6' => ['fd51::1'] }, 'router_mac' => { '4' => router, '6' => router }, 'bridge' => nil, 'tag' => nil }
    end
  end

  def endpoint(family)
    { 'family' => family, 'address' => family == 4 ? '198.51.100.80' : 'fd52::80', 'port' => 18_080, 'url' => 'http://acceptance.test:18080/', 'count' => 1 }
  end

  def calibration(family, _responses)
    captures = %w[client-a client-b].map do |name|
      path = File.join(root, "baseline-#{family}-#{name}-simulator.pcap")
      { 'path' => path, 'sha256' => Digest::SHA256.file(path).hexdigest }
    end
    observed = captures.sum do |capture|
      command('tcpdump', '-nn', '-tt', '-S', '-r', capture.fetch('path')).scan(/Flags \[S\], seq (\d+)/).uniq.size
    end
    raise 'The actual single-provider baseline did not capture two fresh SYNs' unless observed == 2

    reviewed = { 'family' => family, 'mode' => 'random', 'count' => 2, 'weights' => { 'provider' => 1 },
                 'bounds' => { 'provider' => [2, 2] }, 'effective_keys' => 2 }
    artifact = reviewed.merge('observed_counts' => { 'provider' => observed }, 'captures' => captures)
    path = File.join(root, "calibration-#{family}.json")
    File.write(path, JSON.generate(artifact))
    reviewed.merge('baseline' => path, 'baseline_sha256' => Digest::SHA256.file(path).hexdigest)
  end
end

# The plan declares the actual fixture identities and deterministic single-provider calibration.
class MwanAcceptancePlanFixture < MwanAcceptanceDaemonFixture
  include MwanAcceptancePlanInputs

  def setup
    super
    File.write(File.join(root, 'operational-before.json'), JSON.pretty_generate(operational))
    @plan = build_plan
  rescue StandardError
    save_artifacts
    warn command('ip', 'netns', 'exec', namespaces.fetch('gateway'), 'nft', 'list', 'ruleset')
    %w[gateway router simulator client-a].each do |name|
      warn "#{name}: #{command('ip', '-n', namespaces.fetch(name), '-6', '-j', 'route', 'show', 'table', 'all')}"
    end
    raise
  end

  def save_artifacts(label = nil)
    return unless ENV['MWAN_ACCEPTANCE_ARTIFACT_DIR']

    destination = File.join(ENV.fetch('MWAN_ACCEPTANCE_ARTIFACT_DIR'), File.basename(root))
    FileUtils.mkdir_p(destination)
    Dir.glob(File.join(root, '*.{pcap,jsonl,jsonl.gz,json,stderr}')).each { |path| FileUtils.cp(path, destination) }
    FileUtils.cp_r(File.join(root, label), destination) if label
  end

  def baseline(family, address, client_name = 'client-a')
    children = []
    ['gateway', 'router', 'simulator', client_name].each do |name|
      path = File.join(root, "baseline-#{family}-#{client_name}-#{name}")
      command = ['ip', 'netns', 'exec', namespaces.fetch(name), 'tcpdump', '--immediate-mode', '-U', '-nn', '-i', 'any', '-w', "#{path}.pcap", 'tcp', 'port', '18080']
      pid = Process.spawn(*command, out: File::NULL, err: "#{path}.stderr")
      children.push(pid)
      deadline = Process.clock_gettime(Process::CLOCK_MONOTONIC) + 5
      until File.read("#{path}.stderr").include?('listening on')
        raise 'baseline capture did not start' if Process.clock_gettime(Process::CLOCK_MONOTONIC) >= deadline

        sleep 0.05
      end
    end
    command('ip', 'netns', 'exec', namespaces.fetch(client_name), 'curl', "-#{family}", '--noproxy', '*', '--silent', '--show-error', '--fail',
            '--max-time', '5', '--output', File::NULL, '--write-out', MwanAcceptance::CURL_JSON_FORMAT, "http://#{address}:18080/")
  ensure
    stop_baseline(children) if children
  end

  def stop_baseline(children)
    children.each { |pid| Process.kill('INT', pid) }
    wait_baseline(children)
  end

  def wait_baseline(children)
    children.each { |pid| Process.wait(pid) }
  end

  def build_plan
    baseline = File.join(root, 'baseline.json')
    responses = [4, 6].flat_map do |family|
      address = family == 4 ? '198.51.100.80' : '[fd52::80]'
      %w[client-a client-b].map { |name| baseline(family, address, name) }
    end
    File.write(baseline, JSON.generate(responses.map { |response| JSON.parse(response) }))
    { 'ssh_config' => File.join(root, 'ssh_config'), 'targets' => targets, 'gateway' => 'gateway', 'clients' => clients,
      'providers' => [{ 'id' => 'provider', 'interface' => 'enprovider0', 'target' => 'simulator', 'capture_interface' => 'eth0' }],
      'endpoints' => [endpoint(4), endpoint(6)], 'calibrations' => [4, 6].map { |family| calibration(family, baseline) },
      'mappings' => [{ 'provider' => 'provider', 'family' => 4, 'source' => '10.251.1.2', 'external' => '10.251.1.5', 'internal' => '10.251.254.2',
                       'port' => 18_080, 'path' => '/', 'response_sha256' => Digest::SHA256.hexdigest("mwan-acceptance\n") }],
      'history' => { 'directory' => root, 'pattern' => 'ifmgr*.jsonl*', 'service' => service, 'connection' => 'provider', 'family' => 'ipv4',
                     'dependency' => 'wan-routes', 'reason' => 'routing readiness changed', 'seconds' => 30 },
      'transit' => 'enmwanbr0', 'network_json' => '/etc/mwan/network.json', 'binary' => binary, 'binary_sha256' => Digest::SHA256.file(binary).hexdigest,
      'network_sha256' => Digest::SHA256.file('/etc/mwan/network.json').hexdigest, 'capture_seconds' => 60, 'timeout_seconds' => 5,
      'sysrepo' => { 'mode' => 'private', 'repository_path' => "#{root}/installation/etc/sysrepo", 'shm_prefix' => shm_prefix } }
  end

  def invoke(label, document = plan)
    path = File.join(root, "#{label}.json")
    File.write(path, JSON.pretty_generate(document))
    output = File.join(root, label)
    result = CommandRunner.capture([RbConfig.ruby, 'testbed/mwan_acceptance.rb', '--plan', path, '--output', output],
                                   stdin_data: '', chdir: File.expand_path('../..', __dir__), timeout_seconds: 90)
    if ENV['MWAN_ACCEPTANCE_ARTIFACT_DIR']
      destination = File.join(ENV.fetch('MWAN_ACCEPTANCE_ARTIFACT_DIR'), File.basename(root))
      FileUtils.mkdir_p(destination)
      FileUtils.cp_r(output, destination)
    end
    [result, JSON.parse(File.read(File.join(output, 'result.json')))]
  end
end

RSpec.describe 'MWAN downstream acceptance command' do
  before do
    skip 'The real acceptance fixture requires privileged Linux and systemd' unless RUBY_PLATFORM.include?('linux') && Process.uid.zero? && File.directory?('/run/systemd/system')
  end

  def reject_invalid_plans(fixture)
    invalid_plans = %w[targets clients providers endpoints calibrations mappings history sysrepo].map do |key|
      fixture.plan.merge(key => nil)
    end
    invalid_plans.push([])
    invalid_plans.push(fixture.plan.merge('targets' => fixture.plan.fetch('targets') * 2))
    invalid_plans.push(fixture.plan.merge('providers' => fixture.plan.fetch('providers') * 2))
    invalid_plans.each_with_index do |document, index|
      reject_invalid_plan(fixture, document, index)
    end
  end

  def reject_invalid_plan(fixture, document, index)
    path = File.join(fixture.root, "invalid-#{index}.json")
    output = File.join(fixture.root, "invalid-result-#{index}")
    File.write(path, JSON.generate(document))
    invalid = CommandRunner.capture([RbConfig.ruby, 'testbed/mwan_acceptance.rb', '--plan', path, '--output', output],
                                    stdin_data: '', chdir: File.expand_path('../..', __dir__), timeout_seconds: 10)
    expect(invalid.exit_status.exitstatus).to eq(64)
    expect(File.exist?(output)).to be(false)
    expect(invalid.error_output).not_to include('NoMethodError')
  end

  def reject_extra_interfaces(fixture)
    namespace = fixture.namespaces.fetch('client-a')
    controls = [
      ['remote-gre', %w[link add ordinary-gre type gre remote 192.0.2.1], %w[link delete ordinary-gre]],
      ['fallback-address', %w[address add 192.0.2.20/32 dev gre0], %w[address delete 192.0.2.20/32 dev gre0]],
      ['fallback-up', %w[link set gre0 up], %w[link set gre0 down]],
      ['fallback-rule', %w[rule add priority 100 iif gre0 table 100], %w[rule delete priority 100 iif gre0 table 100]]
    ]
    controls.each do |label, add, remove|
      fixture.command('ip', '-n', namespace, *add)
      rejected, rejection = fixture.invoke(label)
      expect(rejected.exit_status.exitstatus).to eq(1)
      expect(rejection.fetch('failure')).to include('extra or missing interface')
      expect(Dir.glob(File.join(fixture.root, "#{label}/*decode*"))).to be_empty
      fixture.command('ip', '-n', namespace, *remove)
    end
  end

  def inject_restart(fixture)
    Thread.new do
      phase = File.join(fixture.root, 'accepted/phase.json')
      result_path = File.join(fixture.root, 'accepted/result.json')
      deadline = Process.clock_gettime(Process::CLOCK_MONOTONIC) + 60
      until File.exist?(phase)
        raise File.read(result_path) if File.exist?(result_path)
        raise 'The public CLI did not begin failure observation' if Process.clock_gettime(Process::CLOCK_MONOTONIC) >= deadline

        sleep 0.05
      end
      fixture.command('ip', '-n', fixture.namespaces.fetch('gateway'), 'route', 'delete', 'default')
      log = File.join(fixture.root, 'ifmgr.jsonl')
      loop do
        transitions = File.readlines(log).map { |line| JSON.parse(line) }
        break if transitions.any? { |record| record['connection_id'] == 'provider' && record.dig('transition', 'Current') == 'not-ready' }
        raise 'The real default withdrawal did not produce failure history' if Process.clock_gettime(Process::CLOCK_MONOTONIC) >= deadline

        sleep 0.05
      end
      fixture.command('ip', '-n', fixture.namespaces.fetch('gateway'), 'route', 'add', 'default', 'via', '10.251.1.2')
      restart_with_rotated_history(fixture, log, deadline)
    end
  end

  def restart_with_rotated_history(fixture, log, deadline)
    File.open(log, 'a') { |file| file.write("\n" * (5 * 1024 * 1024)) }
    fixture.command('systemctl', 'restart', fixture.service)
    until Dir.glob(File.join(fixture.root, 'ifmgr-*.jsonl.gz')).any?
      raise 'The product did not compress its rotated history' if Process.clock_gettime(Process::CLOCK_MONOTONIC) >= deadline

      sleep 0.05
    end
  end

  def reject_history_overrun(fixture)
    archive = Dir.glob(File.join(fixture.root, 'ifmgr-*.jsonl.gz')).first
    copies = Array.new(100) { |index| File.join(fixture.root, "ifmgr-repeat-#{index}.jsonl.gz") }
    copies.each { |path| FileUtils.cp(archive, path) }
    document = Marshal.load(Marshal.dump(fixture.plan))
    document.fetch('history')['seconds'] = 1
    observation = mark_current_archives(fixture, copies)
    result, report = fixture.invoke('history-deadline', document)
    elapsed = Process.clock_gettime(Process::CLOCK_MONOTONIC) - observation.value
    expect(result.exit_status.exitstatus).to eq(1)
    expect(report.fetch('failure')).to include('deadline')
    expect(report.fetch('cleanup_errors')).to be_empty
    expect(elapsed).to be < 3
  ensure
    observation&.join
  end

  def mark_current_archives(fixture, copies)
    phase = File.join(fixture.root, 'history-deadline/phase.json')
    Thread.new do
      deadline = Process.clock_gettime(Process::CLOCK_MONOTONIC) + 60
      until File.exist?(phase)
        raise 'The public CLI did not begin the deadline control' if Process.clock_gettime(Process::CLOCK_MONOTONIC) >= deadline

        sleep 0.01
      end
      started = Process.clock_gettime(Process::CLOCK_MONOTONIC)
      copies.each { |path| File.utime(Time.now, Time.now, path) }
      started
    end
  end

  def interrupt_capture(fixture)
    status, report = capture_control(fixture, 'interrupt') { |pid, _output| Process.kill('INT', pid) }
    expect(status.exitstatus).to eq(130)
    expect(report.fetch('cleanup_errors')).to be_empty
  end

  def stop_capture(fixture)
    status, report = capture_control(fixture, 'stopped-observer') do |_pid, output|
      observer = JSON.parse(File.read(File.join(output, 'capture-phase.json'))).fetch('observer')
      fixture.command('systemctl', 'stop', observer)
    end
    expect(status.exitstatus).to eq(1)
    expect(report.fetch('failure')).to include('not loaded')
    expect(report.fetch('cleanup_errors')).to contain_exactly(report.fetch('failure'))
  end

  def capture_control(fixture, label)
    path = File.join(fixture.root, "#{label}.json")
    output = File.join(fixture.root, label)
    File.write(path, JSON.generate(fixture.plan))
    pid = Process.spawn(RbConfig.ruby, 'testbed/mwan_acceptance.rb', '--plan', path, '--output', output,
                        chdir: File.expand_path('../..', __dir__), pgroup: true,
                        out: File.join(fixture.root, "#{label}.stdout"), err: File.join(fixture.root, "#{label}.stderr"))
    wait_capture(output)
    yield pid, output
    status = CommandRunner.wait_until(pid, Process.clock_gettime(Process::CLOCK_MONOTONIC) + 15)
    raise 'The interrupted command did not finish cleanup' unless status

    pid = nil
    report = JSON.parse(File.read(File.join(output, 'result.json')))
    expect(fixture.command('systemctl', 'list-units', '--all', '--no-legend', 'mwan-acceptance-*')).not_to match(/active\s+running/)
    fixture.save_artifacts(label)
    [status, report]
  ensure
    CommandRunner.kill_group(pid) if pid
  end

  def wait_capture(output)
    deadline = Process.clock_gettime(Process::CLOCK_MONOTONIC) + 60
    until File.exist?(File.join(output, 'capture-phase.json'))
      raise 'The public command did not start its packet observer' if Process.clock_gettime(Process::CLOCK_MONOTONIC) >= deadline

      sleep 0.01
    end
  end

  it 'rejects incomplete input before creating artifacts or starting observers' do
    Dir.mktmpdir do |directory|
      path = File.join(directory, 'plan.json')
      File.write(path, '{}')
      output = File.join(directory, 'result')
      result = CommandRunner.capture([RbConfig.ruby, 'testbed/mwan_acceptance.rb', '--plan', path, '--output', output],
                                     stdin_data: '', chdir: File.expand_path('../..', __dir__), timeout_seconds: 10)
      expect(result.exit_status.exitstatus).to eq(64)
      expect(File.exist?(output)).to be(false)
      expect(result.error_output).to include('ssh_config')
    end
  end

  it 'rejects a real extra OOB interface before starting packet observers' do
    fixture = MwanAcceptancePlanFixture.new
    begin
      fixture.setup
      reject_invalid_plans(fixture)
      interrupt_capture(fixture)
      stop_capture(fixture)
      document = JSON.parse(JSON.generate(fixture.plan))
      document.fetch('endpoints').first['url'] = 'http://acceptance.test:18080/failure'
      failed, failed_report = fixture.invoke('http-failure', document)
      expect(failed.exit_status.exitstatus).to eq(1)
      expect(failed_report.fetch('failure')).to include('503')
      expect(failed_report.fetch('cleanup_errors')).to be_empty
      observers = fixture.command('systemctl', 'list-units', '--all', '--no-legend', 'mwan-acceptance-*')
      expect(observers).not_to match(/active\s+running/)
      reject_extra_interfaces(fixture)
      fixture.command('ip', '-n', fixture.namespaces.fetch('client-a'), 'link', 'add', 'oob0', 'type', 'dummy')
      result, report = fixture.invoke('oob-rejection')
      expect(result.exit_status.exitstatus).to eq(1)
      expect(report.fetch('failure')).to include('extra or missing interface')
      expect(Dir.glob(File.join(fixture.root, 'oob-rejection/*decode*'))).to be_empty
    ensure
      fixture.teardown
    end
  end

  it 'verifies real downstream packets, mapping replies and retained failure history after restart' do
    fixture = MwanAcceptancePlanFixture.new
    fault = nil
    begin
      fixture.setup
      fault = inject_restart(fixture)
      result, report = fixture.invoke('accepted')
      fault.value
      expect(result.exit_status.exitstatus).to eq(0)
      expect(report.fetch('cleanup_errors')).to be_empty
      expect(report.fetch('results').fetch('history').fetch('restart_observed')).to be(true)
      expect(report.fetch('results').fetch('history').fetch('retained_transitions')).not_to be_empty
      reject_history_overrun(fixture)
    ensure
      begin
        fault&.join
      ensure
        fixture.teardown
      end
    end
  end
end
