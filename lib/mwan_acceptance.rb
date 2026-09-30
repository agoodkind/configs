# frozen_string_literal: true

require 'digest'
require 'fileutils'
require 'ipaddr'
require 'json'
require 'securerandom'
require 'shellwords'
require 'time'
require 'uri'
require 'yaml'

module MwanAcceptance
  CURL_JSON_FORMAT = ['%', '{json}'].join.freeze

  class InvalidPlan < StandardError; end
  class Failure < StandardError; end
  class Interrupted < StandardError; end

  Target = Data.define(:id, :host, :adapter, :vmid, :namespace, :hostname, :machine_id, :pve_hostname)
  Client = Data.define(:target, :interface, :mac, :addresses, :gateways, :router_mac, :bridge, :tag)
  Provider = Data.define(:id, :interface, :target, :capture_interface)
  Endpoint = Data.define(:family, :address, :port, :url, :count)
  Calibration = Data.define(:family, :mode, :count, :weights, :bounds, :baseline, :baseline_sha256, :effective_keys)
  Mapping = Data.define(:provider, :family, :source, :external, :internal, :port, :path, :response_sha256)
  History = Data.define(:directory, :pattern, :service, :connection, :family, :dependency, :reason, :seconds)

  # Input boundaries reject unsupported values before remote commands.
  module InputValidation
    def require_type(value, type, field)
      raise InvalidPlan, "#{field} must be #{type}" unless value.is_a?(type)
    end

    def local_file(path)
      raise InvalidPlan, "required local file is not regular: #{path}" unless File.file?(path)

      File.expand_path(path)
    end

    def validate_address(value, family)
      require_type(value, String, 'address')
      raise InvalidPlan, 'address requires IPv4 or IPv6 family' unless [4, 6].include?(family)
      raise InvalidPlan, 'address family differs' unless IPAddr.new(value).ipv4? == (family == 4)
    end
  end

  # Plan containers and record scalar types are validated before construction.
  class PlanShape
    include InputValidation

    def initialize(document)
      @document = document
    end

    def validate
      validate_document(@document)
    end

    private

    def validate_document(document)
      require_type(document, Hash, 'plan')
      fields = %w[ssh_config targets gateway clients providers endpoints calibrations mappings history transit network_json binary
                  binary_sha256 network_sha256 sysrepo capture_seconds timeout_seconds]
      raise InvalidPlan, 'plan contains unsupported fields' unless (document.keys - fields).empty?

      %w[ssh_config gateway transit network_json binary binary_sha256 network_sha256].each do |key|
        require_type(document.fetch(key), String, key)
      end
      %w[targets clients providers endpoints calibrations mappings].each do |key|
        require_type(document.fetch(key), Array, key)
        document.fetch(key).each { |entry| require_type(entry, Hash, "#{key} entry") }
      end
      %w[history sysrepo].each { |key| require_type(document.fetch(key), Hash, key) }
      %w[capture_seconds timeout_seconds].each { |key| require_type(document.fetch(key), Integer, key) }
      validate_record_shapes(document)
      %w[targets providers].each do |key|
        identifiers = document.fetch(key).map { |entry| entry.fetch('id') }
        raise InvalidPlan, "#{key} contains duplicate ids" unless identifiers.uniq.size == identifiers.size
      end
    end

    def validate_record_shapes(document)
      validate_record_keys(document)
      validate_strings(document)
      validate_numeric_containers(document)
      raise InvalidPlan, 'history contains unsupported fields' unless (document.fetch('history').keys - History.members.map(&:to_s)).empty?

      %w[directory pattern service connection family dependency reason].each do |key|
        require_type(document.fetch('history').fetch(key), String, "history.#{key}")
      end
      require_type(document.fetch('history').fetch('seconds'), Integer, 'history.seconds')
      require_type(document.fetch('sysrepo').fetch('mode'), String, 'sysrepo.mode')
    end

    def validate_record_keys(document)
      { 'targets' => Target, 'clients' => Client, 'providers' => Provider, 'endpoints' => Endpoint,
        'calibrations' => Calibration, 'mappings' => Mapping }.each do |section, record|
        document.fetch(section).each do |entry|
          raise InvalidPlan, "#{section} contains unsupported fields" unless (entry.keys - record.members.map(&:to_s)).empty?
        end
      end
    end

    def validate_strings(document)
      strings = {
        'targets' => %w[id host adapter hostname], 'clients' => %w[target interface mac],
        'providers' => %w[id interface target capture_interface], 'endpoints' => %w[address url],
        'calibrations' => %w[mode baseline baseline_sha256], 'mappings' => %w[provider source external internal path response_sha256]
      }
      strings.each do |section, fields|
        document.fetch(section).each { |entry| fields.each { |key| require_type(entry.fetch(key), String, "#{section}.#{key}") } }
      end
    end

    def validate_numeric_containers(document)
      %w[endpoints calibrations mappings].each do |section|
        document.fetch(section).each { |entry| require_type(entry.fetch('family'), Integer, "#{section}.family") }
      end
      %w[endpoints mappings].each do |section|
        document.fetch(section).each { |entry| require_type(entry.fetch('port'), Integer, "#{section}.port") }
      end
      document.fetch('clients').each { |entry| %w[addresses gateways router_mac].each { |key| require_type(entry.fetch(key), Hash, "clients.#{key}") } }
      document.fetch('calibrations').each do |entry|
        %w[weights bounds].each { |key| require_type(entry.fetch(key), Hash, "calibrations.#{key}") }
        %w[count effective_keys].each { |key| require_type(entry.fetch(key), Integer, "calibrations.#{key}") }
      end
    end
  end

  # Identity records bind clients and providers to declared remote targets.
  class PlanRecords
    include InputValidation

    attr_reader :targets

    def initialize(document)
      @targets = document.fetch('targets').map { |entry| target(entry) }.to_h { |entry| [entry.id, entry] }
    end

    def target(entry)
      value = Target.new(**symbolize(entry))
      raise InvalidPlan, "unsupported identity adapter #{value.adapter}" unless %w[proxmox linux].include?(value.adapter)

      validate_adapter(value)
      raise InvalidPlan, 'target id must be a safe identifier' unless /\A[a-zA-Z0-9_-]+\z/.match?(value.id)
      raise InvalidPlan, 'SSH host must be nonempty and must not be an option' unless !value.host.empty? && !value.host.start_with?('-')
      raise InvalidPlan, 'namespace must be absent or a safe identifier' unless value.namespace.nil? || (value.namespace.is_a?(String) && /\A[a-zA-Z0-9_-]+\z/.match?(value.namespace))

      value
    end

    def validate_adapter(value)
      if value.adapter == 'proxmox'
        raise InvalidPlan, 'Proxmox requires a positive VMID and no namespace' unless value.vmid.is_a?(Integer) && value.vmid.positive? && value.namespace.nil?
        raise InvalidPlan, 'Proxmox requires an explicit hostname' unless value.pve_hostname.is_a?(String) && !value.pve_hostname.empty?
      else
        require_type(value.machine_id, String, 'machine_id')
        raise InvalidPlan, 'Linux identity requires machine_id' unless /\A[0-9a-f]{32}\z/.match?(value.machine_id)
      end
    end

    def client(entry)
      target = @targets.fetch(entry.fetch('target'))
      if target.adapter == 'proxmox'
        require_type(entry.fetch('bridge'), String, 'clients.bridge')
        require_type(entry.fetch('tag'), Integer, 'clients.tag')
        raise InvalidPlan, 'client VLAN tag must be between1 and4094' unless entry.fetch('tag').between?(1, 4094)
      end
      validate_client_families(entry)
      validate_mac(entry.fetch('mac'))
      Client.new(target: target, interface: entry.fetch('interface'), mac: entry.fetch('mac'),
                 addresses: entry.fetch('addresses'), gateways: entry.fetch('gateways'), router_mac: entry.fetch('router_mac'), bridge: entry.fetch('bridge'), tag: entry.fetch('tag'))
    end

    def validate_client_families(entry)
      %w[addresses gateways router_mac].each do |key|
        raise InvalidPlan, "clients.#{key} requires both families" unless entry.fetch(key).keys.sort == %w[4 6]
      end
      %w[4 6].each do |family|
        require_type(entry.fetch('addresses').fetch(family), String, "clients.addresses.#{family}")
        require_type(entry.fetch('router_mac').fetch(family), String, "clients.router_mac.#{family}")
        validate_mac(entry.fetch('router_mac').fetch(family))
        aliases = entry.fetch('gateways').fetch(family)
        require_type(aliases, Array, "clients.gateways.#{family}")
        raise InvalidPlan, 'client gateway aliases cannot be empty' if aliases.empty?

        aliases.each { |address| validate_address(address, family.to_i) }
        validate_address(entry.fetch('addresses').fetch(family), family.to_i)
      end
    end

    def validate_mac(value)
      raise InvalidPlan, 'MAC requires six hexadecimal octets' unless /\A(?:[0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}\z/.match?(value)
    end

    def provider(entry)
      raise InvalidPlan, 'provider id must be a safe identifier' unless /\A[a-zA-Z0-9_-]+\z/.match?(entry.fetch('id'))

      Provider.new(id: entry.fetch('id'), interface: entry.fetch('interface'), target: @targets.fetch(entry.fetch('target')),
                   capture_interface: entry.fetch('capture_interface'))
    end

    def symbolize(entry)
      entry.transform_keys(&:to_sym)
    end
  end

  # Calibration artifacts bind reviewed policy to real capture hashes and counts.
  class CalibrationValidation
    include InputValidation

    def validate(entry)
      validate_calibration(entry)
    end

    private

    def validate_calibration(entry)
      local_file(entry.baseline)
      raise InvalidPlan, 'calibration baseline hash differs' unless Digest::SHA256.file(entry.baseline).hexdigest == entry.baseline_sha256
      raise InvalidPlan, 'calibration requires positive effective key population' unless entry.effective_keys.is_a?(Integer) && entry.effective_keys.positive?

      sample_size = entry.count
      raise InvalidPlan, 'calibration count must be positive' unless sample_size.positive?
      raise InvalidPlan, 'unsupported balancing mode' unless %w[random source source-destination].include?(entry.mode)
      raise InvalidPlan, 'calibration weights and bounds differ' unless entry.weights.keys.sort == entry.bounds.keys.sort
      raise InvalidPlan, 'calibration weights must be positive integers' unless entry.weights.values.all? { |value| value.is_a?(Integer) && value.positive? }

      validate_bounds(entry)

      validate_baseline(entry)
    end

    def validate_bounds(entry)
      valid = entry.bounds.values.all? do |value|
        value.is_a?(Array) && value.size == 2 && value.all? { |number| number.is_a?(Integer) && number >= 0 } &&
          value[0] <= value[1] && value[1] <= entry.count
      end
      raise InvalidPlan, 'invalid calibrated count bounds' unless valid
    end

    def validate_baseline(entry)
      baseline = JSON.parse(File.read(entry.baseline))
      require_type(baseline, Hash, 'calibration baseline')
      %w[family mode count weights bounds effective_keys].each do |key|
        raise InvalidPlan, "calibration baseline #{key} differs" unless baseline.fetch(key) == entry.public_send(key)
      end
      counts = baseline.fetch('observed_counts')
      require_type(counts, Hash, 'calibration observed_counts')
      raise InvalidPlan, 'calibration observed providers differ' unless counts.keys.sort == entry.weights.keys.sort
      raise InvalidPlan, 'calibration observed counts must be nonnegative integers' unless counts.values.all? { |value| value.is_a?(Integer) && value >= 0 }
      raise InvalidPlan, 'calibration observed sample count differs' unless counts.values.sum == entry.count

      entry.bounds.each do |provider, bounds|
        raise InvalidPlan, "calibration baseline #{provider} outside reviewed bounds" unless counts.fetch(provider).between?(*bounds)
      end
      validate_captures(baseline.fetch('captures'))
    end

    def validate_captures(captures)
      require_type(captures, Array, 'calibration captures')
      raise InvalidPlan, 'calibration captures are required' if captures.empty?

      captures.each do |capture|
        require_type(capture, Hash, 'calibration capture')
        require_type(capture.fetch('path'), String, 'calibration capture path')
        require_type(capture.fetch('sha256'), String, 'calibration capture hash')
        local_file(capture.fetch('path'))
        raise InvalidPlan, 'calibration capture hash differs' unless Digest::SHA256.file(capture.fetch('path')).hexdigest == capture.fetch('sha256')
      end
    end
  end

  # The plan provides validated identities, policy and observation limits.
  class Plan
    include InputValidation

    attr_reader :ssh_config, :targets, :clients, :providers, :endpoints, :calibrations, :mappings, :history, :gateway,
                :transit, :network_json, :binary, :binary_sha256, :network_sha256, :sysrepo, :capture_seconds, :timeout_seconds

    def initialize(path)
      document = JSON.parse(File.read(path))
      PlanShape.new(document).validate
      @ssh_config = local_file(document.fetch('ssh_config'))
      records = PlanRecords.new(document)
      @targets = records.targets
      @gateway = @targets.fetch(document.fetch('gateway'))
      @clients = document.fetch('clients').map { |entry| records.client(entry) }
      @providers = document.fetch('providers').map { |entry| records.provider(entry) }
      @endpoints = document.fetch('endpoints').map { |entry| Endpoint.new(**records.symbolize(entry)) }
      @calibrations = document.fetch('calibrations').map { |entry| Calibration.new(**records.symbolize(entry)) }
      @mappings = document.fetch('mappings').map { |entry| Mapping.new(**records.symbolize(entry)) }
      @history = History.new(**records.symbolize(document.fetch('history')))
      read_settings(document)
      validate
    rescue KeyError, TypeError, ArgumentError, JSON::ParserError, Errno::ENOENT => e
      raise InvalidPlan, e.message
    end

    private

    def read_settings(document)
      %w[transit network_json binary binary_sha256 network_sha256 sysrepo capture_seconds timeout_seconds].each do |key|
        instance_variable_set("@#{key}", document.fetch(key))
      end
    end

    def validate
      raise InvalidPlan, 'exactly two clients are required' unless @clients.size == 2
      raise InvalidPlan, 'client targets must differ' unless @clients.map { |client| client.target.id }.uniq.size == 2
      raise InvalidPlan, 'both address families are required' unless @endpoints.map(&:family).sort == [4, 6]
      raise InvalidPlan, 'providers and mappings are required' if @providers.empty? || @mappings.empty?

      validate_deadlines
      validate_settings
      validate_sysrepo
      @endpoints.each { |entry| validate_endpoint(entry) }
      @calibrations.each { |entry| CalibrationValidation.new.validate(entry) }
      @mappings.each { |entry| validate_mapping(entry) }
      raise InvalidPlan, 'both families require calibration' unless @calibrations.map(&:family).sort == [4, 6]
    end

    def validate_settings
      raise InvalidPlan, 'product and history paths must be absolute' unless [@binary, @network_json, @history.directory].all? { |path| path.start_with?('/') }
      raise InvalidPlan, 'history family must be ipv4 or ipv6' unless %w[ipv4 ipv6].include?(@history.family)
      raise InvalidPlan, 'history pattern must select files in one directory' if @history.pattern.empty? || @history.pattern.include?('/')

      @calibrations.each do |entry|
        raise InvalidPlan, 'calibration provider is not declared' unless (entry.weights.keys - @providers.map(&:id)).empty?

        endpoint = @endpoints.find { |value| value.family == entry.family }
        raise InvalidPlan, 'calibration count differs from the HTTP cohort' unless endpoint && entry.count == endpoint.count * @clients.size
      end
    end

    def validate_mapping(entry)
      raise InvalidPlan, 'mapping provider is not declared' unless @providers.any? { |provider| provider.id == entry.provider }

      [entry.source, entry.external, entry.internal].each { |value| validate_address(value, entry.family) }
      raise InvalidPlan, 'mapping port must be between 1 and 65535' unless entry.port.between?(1, 65_535)
      raise InvalidPlan, 'mapping path must start with /' unless entry.path.start_with?('/')
      raise InvalidPlan, 'mapping response hash must be SHA256' unless /\A[0-9a-f]{64}\z/.match?(entry.response_sha256)
    end

    def validate_deadlines
      raise InvalidPlan, 'deadlines must be positive integers' unless [@capture_seconds, @timeout_seconds, @history.seconds].all? { |value| value.is_a?(Integer) && value.positive? }
      raise InvalidPlan, 'HTTP timeout must be at least two seconds' unless @timeout_seconds >= 2

      valid = @endpoints.all? do |entry|
        sample_size = entry.count
        sample_size.is_a?(Integer) && sample_size.positive? && @capture_seconds > sample_size * @timeout_seconds
      end
      raise InvalidPlan, 'capture deadline must exceed each HTTP cohort' unless valid
      raise InvalidPlan, 'binary and network hashes must be SHA256' unless [@binary_sha256, @network_sha256].all? { |value| /\A[0-9a-f]{64}\z/.match?(value) }
    end

    def validate_sysrepo
      return if @sysrepo == { 'mode' => 'deployment-default' }

      if @sysrepo.keys.sort == %w[mode repository_path shm_prefix] && @sysrepo['mode'] == 'private'
        require_type(@sysrepo.fetch('repository_path'), String, 'sysrepo.repository_path')
        require_type(@sysrepo.fetch('shm_prefix'), String, 'sysrepo.shm_prefix')
        raise InvalidPlan, 'private sysrepo repository_path must be absolute' unless @sysrepo.fetch('repository_path').start_with?('/')
        raise InvalidPlan, 'private sysrepo shm_prefix must be a safe identifier' unless /\A[a-zA-Z0-9_-]+\z/.match?(@sysrepo.fetch('shm_prefix'))

        return
      end

      raise InvalidPlan, 'sysrepo requires explicit deployment-default mode or private repository_path and shm_prefix'
    end

    def validate_endpoint(entry)
      validate_address(entry.address, entry.family)
      uri = URI(entry.url)
      acceptable = %w[http https].include?(uri.scheme) && uri.host && uri.port == entry.port && entry.port.between?(1, 65_535)
      raise InvalidPlan, 'endpoint requires HTTP or HTTPS and an explicit matching port' unless acceptable
    end
  end

  ProcessResult = Data.define(:stdout, :stderr, :status)

  # Process groups enforce deadlines and preserve command evidence.
  class Processes
    attr_reader :interrupted

    def initialize(directory)
      @directory = directory
      @children = {}
      @sequence = 0
      @interrupted = false
    end

    def interrupt
      @interrupted = true
    end

    def start(argv, label)
      @sequence += 1
      stem = File.join(@directory, format('%<sequence>04d-%<label>s', sequence: @sequence, label: label))
      File.write("#{stem}.command.json", JSON.generate(argv))
      output = File.open("#{stem}.stdout", 'wb')
      error = File.open("#{stem}.stderr", 'wb')
      pid = Process.spawn(*argv, pgroup: true, out: output, err: error)
      @children[pid] = stem
      [pid, stem]
    ensure
      output&.close
      error&.close
    end

    def run(argv, label, seconds, cleanup: false)
      pid, stem = start(argv, label)
      status = wait(pid, seconds, cleanup: cleanup)
      result = ProcessResult.new(stdout: File.binread("#{stem}.stdout"), stderr: File.binread("#{stem}.stderr"), status: status)
      raise Failure, "#{label} failed (#{status.exitstatus}): #{result.stderr[-2000..] || result.stderr}" unless status.success?

      result
    end

    def wait(pid, seconds, cleanup: false)
      deadline = monotonic + seconds
      loop do
        raise Interrupted, 'acceptance interrupted' if @interrupted && !cleanup

        pair = Process.wait2(pid, Process::WNOHANG)
        if pair
          record_status(pid, pair[1], cleanup)
          @children.delete(pid)
          return pair[1]
        end
        raise Failure, "process #{pid} exceeded #{seconds}s deadline" if monotonic >= deadline

        sleep 0.05
      end
    end

    def running?(pid)
      Process.kill(0, pid)
      true
    rescue Errno::ESRCH
      false
    end

    def cleanup
      @children.each_key { |pid| signal_group(pid, 'TERM') }
      sleep 0.1 unless @children.empty?
      @children.each_key do |pid|
        signal_group(pid, 'KILL')
        _child, status = Process.wait2(pid)
        record_status(pid, status, true)
      end
      @children.clear
    end

    def monotonic
      Process.clock_gettime(Process::CLOCK_MONOTONIC)
    end

    private

    def signal_group(pid, signal)
      Process.kill(signal, -pid)
    rescue Errno::ESRCH
      nil
    end

    def record_status(pid, status, cleanup)
      stem = @children.fetch(pid)
      File.write("#{stem}.status.json", JSON.generate({ pid: pid, exit_status: status.exitstatus, signal: status.termsig, cleanup: cleanup, ended: Time.now.iso8601(9) }))
    end
  end

  # SSH commands use the declared Linux or Proxmox identity adapter.
  class Remote
    def initialize(plan, processes)
      @plan = plan
      @processes = processes
    end

    def prefix(target)
      return ['pct', 'exec', target.vmid.to_s, '--'] if target.adapter == 'proxmox'
      return ['ip', 'netns', 'exec', target.namespace] if target.namespace

      []
    end

    def argv(target, command, guest: true)
      command = prefix(target) + command if guest
      ['ssh', '-F', @plan.ssh_config, '-o', 'BatchMode=yes', '-o', "ConnectTimeout=#{@plan.timeout_seconds}", target.host, Shellwords.join(command)]
    end

    def read(target, command, label, guest: true, cleanup: false)
      @processes.run(argv(target, command, guest: guest), label, @plan.timeout_seconds, cleanup: cleanup).stdout
    end

    def json(target, command, label)
      JSON.parse(read(target, command, label))
    rescue JSON::ParserError => e
      raise Failure, "#{label} returned invalid JSON: #{e.message}"
    end

    def identity(target)
      hostname = read(target, ['hostname'], "#{target.id}-hostname").strip
      raise Failure, "#{target.id}: hostname #{hostname} differs from #{target.hostname}" unless hostname == target.hostname

      if target.adapter == 'proxmox'
        configuration = read(target, ['pct', 'config', target.vmid.to_s], "#{target.id}-pct-config", guest: false)
        raise Failure, "#{target.id}: pct hostname differs" unless configuration.lines.include?("hostname: #{target.pve_hostname}\n")
      else
        identity = read(target, ['cat', '/etc/machine-id'], "#{target.id}-machine-id").strip
        raise Failure, "#{target.id}: machine identity differs" unless identity == target.machine_id
      end
    end
  end

  # The preflight rejects alternative client egress before packet observers start.
  module ClientFallbacks
    FALLBACKS = {
      'tunl0' => ['ipip', { 'proto' => 'any', 'remote' => 'any', 'local' => 'any', 'ttl' => 0, 'pmtudisc' => false }],
      'gre0' => ['gre', { 'remote' => 'any', 'local' => 'any', 'ttl' => 0, 'pmtudisc' => false }],
      'gretap0' => ['gretap', { 'remote' => 'any', 'local' => 'any', 'ttl' => 0, 'pmtudisc' => false }],
      'erspan0' => ['erspan', { 'remote' => 'any', 'local' => 'any', 'ttl' => 0, 'pmtudisc' => false, 'okey' => '0.0.0.0', 'erspan_index' => 0, 'erspan_ver' => 1 }],
      'ip_vti0' => ['vti', { 'remote' => 'any', 'local' => 'any' }],
      'ip6_vti0' => ['vti6', { 'remote' => 'any', 'local' => 'any' }],
      'sit0' => ['sit', { 'proto' => 'ip6ip', 'remote' => 'any', 'local' => 'any', 'ttl' => 64, 'pmtudisc' => false, 'prefix' => '2002::', 'prefixlen' => 16 }],
      'ip6tnl0' => ['ip6tnl', { 'proto' => 'ip6ip6', 'remote' => 'any', 'local' => 'any', 'ttl' => 0, 'encap_limit' => 0, 'tclass' => '0x00', 'flowlabel' => '0x00000' }],
      'ip6gre0' => ['ip6gre', { 'remote' => 'any', 'local' => 'any', 'ttl' => 0, 'encap_limit' => 0, 'tclass' => '0x00', 'flowlabel' => '0x00000' }]
    }.freeze

    def attachments(client, links, addresses)
      routes = []
      rules = []
      [4, 6].each do |family|
        routes.concat(@remote.json(client.target, ['ip', "-#{family}", '-j', 'route', 'show', 'table', 'all'], "#{client.target.id}-fallback-routes#{family}"))
        rules.concat(@remote.json(client.target, ['ip', "-#{family}", '-j', 'rule'], "#{client.target.id}-fallback-rules#{family}"))
      end
      links.reject do |link|
        name = link.fetch('ifname')
        expected = FALLBACKS[name]
        next false unless expected
        next false unless link.fetch('operstate') == 'DOWN' && !link.fetch('flags').include?('UP') && !link.key?('master')
        next false unless link.dig('linkinfo', 'info_kind') == expected[0] && link.dig('linkinfo', 'info_data') == expected[1]
        next false unless addresses.find { |item| item.fetch('ifname') == name }.fetch('addr_info').empty?

        unused_fallback?(name, routes, rules)
      end
    end

    def unused_fallback?(name, routes, rules)
      return false if routes.any? { |route| route['dev'] == name || route.fetch('nexthops', []).any? { |hop| hop['dev'] == name } }
      return false if rules.any? { |rule| [rule['iif'], rule['oif'], rule['iifname'], rule['oifname']].include?(name) }

      true
    end
  end

  # Client attachments and selected routes must match the declared router.
  class ClientPreflight
    include ClientFallbacks

    def initialize(remote, plan)
      @remote = remote
      @plan = plan
    end

    def verify(client)
      @remote.identity(client.target)
      verify_proxmox(client) if client.target.adapter == 'proxmox'
      links = @remote.json(client.target, %w[ip -j -d link], "#{client.target.id}-links").reject { |link| link.fetch('ifname') == 'lo' }
      addresses = @remote.json(client.target, %w[ip -j address], "#{client.target.id}-addresses")
      links = attachments(client, links, addresses) if client.target.adapter == 'linux'
      verify_attachment(client, links, addresses)
      @plan.endpoints.each { |endpoint| family(client, endpoint) }
    end

    private

    def verify_attachment(client, links, addresses)
      raise Failure, "#{client.target.id}: extra or missing interface" unless links.size == 1 && links[0].fetch('ifname') == client.interface
      raise Failure, "#{client.target.id}: MAC differs" unless links[0].fetch('address').casecmp?(client.mac)

      observed = addresses.flat_map { |link| link.fetch('addr_info').map { |address| address.fetch('local') } }
      client.addresses.each_value { |address| raise Failure, "#{client.target.id}: address #{address} missing" unless observed.any? { |actual| IPAddr.new(actual) == IPAddr.new(address) } }
    end

    def verify_proxmox(client)
      configuration = @remote.read(client.target, ['pct', 'config', client.target.vmid.to_s], "#{client.target.id}-client-pct", guest: false)
      values = configuration.lines.to_h do |line|
        key, value = line.split(':', 2)
        [key, value.strip]
      end
      interfaces = values.keys.grep(/\Anet\d+\z/)
      raise Failure, "#{client.target.id}: Proxmox requires exactly net0" unless interfaces == ['net0']
      raise Failure, "#{client.target.id}: Proxmox client must be unprivileged" unless values.fetch('unprivileged') == '1'

      verify_proxmox_interface(client, values.fetch('net0').split(',').to_h { |field| field.split('=', 2) })
    end

    def verify_proxmox_interface(client, interface)
      expected = { 'name' => client.interface, 'bridge' => client.bridge, 'tag' => client.tag.to_s }
      expected.each { |key, value| raise Failure, "#{client.target.id}: Proxmox #{key} differs" unless interface.fetch(key) == value }
      raise Failure, "#{client.target.id}: Proxmox MAC differs" unless interface.fetch('hwaddr').casecmp?(client.mac)

      { '4' => 'ip', '6' => 'ip6' }.each do |family, key|
        actual = IPAddr.new(interface.fetch(key).split('/').first)
        raise Failure, "#{client.target.id}: Proxmox IPv#{family} differs" unless actual == IPAddr.new(client.addresses.fetch(family))
      end
    end

    def family(client, endpoint)
      flag = "-#{endpoint.family}"
      label = "#{client.target.id}-ipv#{endpoint.family}"
      routes = @remote.json(client.target, ['ip', flag, '-j', 'route', 'show', 'table', 'all'], "#{label}-routes")
      defaults = routes.select { |route| route['dst'] == 'default' }
      aliases = client.gateways.fetch(endpoint.family.to_s)
      accepted = defaults.any? && defaults.all? { |route| route['dev'] == client.interface && aliases.any? { |address| IPAddr.new(route.fetch('gateway')) == IPAddr.new(address) } }
      raise Failure, "#{label}: alternate or missing default route" unless accepted

      verify_router(client, endpoint.family, aliases)
      verify_selection(client, endpoint, label, aliases)
    end

    def verify_selection(client, endpoint, label, aliases)
      flag = "-#{endpoint.family}"
      rules = @remote.json(client.target, ['ip', flag, '-j', 'rule'], "#{label}-rules")
      raise Failure, "#{label}: custom routing rule" unless rules.all? { |rule| %w[local main default].include?(rule.fetch('table').to_s) }

      selection = @remote.json(client.target, ['ip', flag, '-j', 'route', 'get', endpoint.address, 'from', client.addresses.fetch(endpoint.family.to_s)], "#{label}-route-get")
      accepted = selection.size == 1 && selection[0]['dev'] == client.interface && aliases.any? { |address| IPAddr.new(selection[0].fetch('gateway')) == IPAddr.new(address) }
      raise Failure, "#{label}: endpoint uses unexpected route" unless accepted
    end

    def verify_router(client, family, aliases)
      aliases.each do |address|
        @remote.read(client.target, ['ping', "-#{family}", '-I', client.interface, '-c', '1', '-W', @plan.timeout_seconds.to_s, address], "#{client.target.id}-router-probe")
      end
      neighbors = @remote.json(client.target, ['ip', "-#{family}", '-j', 'neighbor', 'show', 'dev', client.interface], "#{client.target.id}-router-neighbors")
      aliases.each do |address|
        neighbor = neighbors.find { |entry| IPAddr.new(entry.fetch('dst')) == IPAddr.new(address) }
        raise Failure, "#{client.target.id}: router identity differs for #{address}" unless neighbor&.fetch('lladdr')&.casecmp?(client.router_mac.fetch(family.to_s))
      end
    end
  end
  Observer = Data.define(:target, :unit, :pid, :stem, :label)

  # Packet observers use bounded systemd units and require kernel drop counters.
  class Captures
    attr_reader :observers

    def initialize(remote, processes, plan, run_id)
      @remote = remote
      @processes = processes
      @plan = plan
      @run_id = run_id
      @observers = []
      @finished = {}
    end

    def start(target, interface, label, port)
      unit = "mwan-acceptance-#{@run_id}-#{label}"
      command = ['systemd-run', '--quiet', '--collect', '--pipe', '--wait', "--unit=#{unit}", "--property=RuntimeMaxSec=#{@plan.capture_seconds}",
                 '--property=KillSignal=SIGINT', '--property=TimeoutStopSec=3', '--']
      command += @remote.prefix(target) + ['tcpdump', '--immediate-mode', '-U', '-nn', '-s', '0', '-i', interface, '-w', '-', 'tcp', 'port', port.to_s]
      pid, stem = @processes.start(@remote.argv(target, command, guest: false), label)
      observer = Observer.new(target: target, unit: unit, pid: pid, stem: stem, label: label)
      @observers.push(observer)
      deadline = @processes.monotonic + @plan.timeout_seconds
      loop do
        raise Interrupted, 'acceptance interrupted' if @processes.interrupted
        return observer if File.read("#{stem}.stderr").include?('listening on')
        raise Failure, "#{label}: capture did not start" unless @processes.running?(pid) && @processes.monotonic < deadline

        sleep 0.05
      end
    end

    def stop
      errors = []
      @observers.each do |observer|
        stop_one(observer)
      rescue Failure => e
        errors.push(e.message)
      end
      raise Failure, errors.join("\n") unless errors.empty?
    end

    def packets(observer)
      @processes.run(['tcpdump', '-nn', '-tt', '-S', '-r', "#{observer.stem}.stdout"], "#{observer.label}-decode", @plan.timeout_seconds).stdout
    end

    private

    def stop_one(observer)
      return if @finished[observer.unit]

      @remote.read(observer.target, ['systemctl', 'stop', observer.unit], "#{observer.label}-stop", guest: false, cleanup: true)
      status = @processes.wait(observer.pid, @plan.timeout_seconds, cleanup: true)
      state = @remote.read(observer.target, ['systemctl', 'show', observer.unit, '--property=ActiveState', '--value'], "#{observer.label}-inactive", guest: false, cleanup: true).strip
      raise Failure, "#{observer.label}: observer remains #{state}" unless %w[inactive failed].include?(state)

      @finished[observer.unit] = true
      raise Failure, "#{observer.label}: capture exited #{status.exitstatus}" unless status.success?

      counters = File.read("#{observer.stem}.stderr")
      raise Failure, "#{observer.label}: capture omitted kernel drop counters" unless counters.match?(/^0 packets dropped by kernel$/)
    end
  end

  # Runtime exports and kernel snapshots verify the deployed ownership source.
  class Product
    def initialize(remote, plan)
      @remote = remote
      @plan = plan
    end

    def snapshot
      environment = []
      environment = ['env', "SYSREPO_REPOSITORY_PATH=#{@plan.sysrepo.fetch('repository_path')}", "SYSREPO_SHM_PREFIX=#{@plan.sysrepo.fetch('shm_prefix')}"] if @plan.sysrepo.fetch('mode') == 'private'
      @remote.json(@plan.gateway, environment + %w[sysrepocfg -X -d operational -m ietf-interfaces -f json], 'operational-interfaces')
    end

    def verify
      @remote.identity(@plan.gateway)
      verify_hash(@plan.binary, @plan.binary_sha256, 'deployed-binary')
      verify_hash(@plan.network_json, @plan.network_sha256, 'deployed-network')
      tree = snapshot.fetch('ietf-interfaces:interfaces')
      configured = @remote.json(@plan.gateway, ['cat', @plan.network_json], 'network-intent').fetch('ietf-interfaces:interfaces')
      links = @remote.json(@plan.gateway, %w[ip -j link], 'gateway-links')
      @remote.json(@plan.gateway, %w[ip -j address], 'gateway-addresses')
      [4, 6].each do |family|
        @remote.json(@plan.gateway, ['ip', "-#{family}", '-j', 'route', 'show', 'table', 'all'], "gateway-ipv#{family}-routes")
        @remote.json(@plan.gateway, ['ip', "-#{family}", '-j', 'rule'], "gateway-ipv#{family}-rules")
      end
      @remote.json(@plan.gateway, %w[nft -j list ruleset], 'gateway-nftables')
      @plan.providers.each { |provider| verify_owner(provider, tree, configured, links) }
      [tree, configured]
    end

    private

    def verify_hash(path, expected, label)
      actual = @remote.read(@plan.gateway, ['sha256sum', path], label).split.first
      raise Failure, "#{label}: SHA256 differs" unless actual == expected
    end

    def verify_owner(provider, tree, configured, links)
      actual = tree.fetch('interface').find { |entry| entry.fetch('name') == provider.interface }
      intent = configured.fetch('interface').find { |entry| entry.fetch('name') == provider.interface }
      raise Failure, "#{provider.id}: configured or served interface missing" unless actual && intent

      state = actual.fetch('goodkind-mwan-steering:ownership-state')
      verify_identity(provider, intent, state, links)
      [4, 6].each { |family| verify_assignments(actual, provider, family) }
    end

    def verify_identity(provider, intent, state, links)
      expected_id = intent['goodkind-mwan-steering:connection-id'] || intent.fetch('goodkind-mwan-steering:wan').fetch('name')
      expected_owner = intent.fetch('goodkind-mwan-steering:owner')
      link = links.find { |entry| entry.fetch('ifname') == provider.interface }
      raise Failure, "#{provider.id}: owner or stable identity differs" unless state.fetch('configured-owner') == expected_owner && state.fetch('connection-id') == expected_id
      raise Failure, "#{provider.id}: served/kernel identity differs" unless link && state.fetch('actual-name') == provider.interface && state.fetch('actual-index') == link.fetch('ifindex')
      raise Failure, "#{provider.id}: observation is not fresh" unless state.fetch('observation') == 'fresh'

      apply = state['last-apply']
      raise Failure, "#{provider.id}: last apply failed: #{apply.fetch('reason', '')}" if apply && apply['result'] == 'failed'
    end

    def verify_assignments(actual, provider, family)
      state = actual.fetch("ietf-ip:ipv#{family}", {})['goodkind-mwan-steering:ownership-family-state']
      return unless state

      apply = state['last-apply']
      raise Failure, "#{provider.id}/ipv#{family}: failed dependency #{apply['dependency']}: #{apply['reason']}" if apply && apply['result'] == 'failed'

      state.fetch('assignment', []).each do |assignment|
        next unless assignment.fetch('valid')
        next unless assignment['valid-until']

        raise Failure, "#{provider.id}/ipv#{family}: expired assignment #{assignment['id']}" unless Time.iso8601(assignment['valid-until']) > Time.now
      end
    end
  end

  # Provider eligibility requires health, family routing and translation readiness.
  class Policy
    def initialize(plan, tree, configured)
      @plan = plan
      @tree = tree
      @configured = configured
    end

    def calibration(family)
      calibration = @plan.calibrations.find { |entry| entry.family == family }
      mode = @configured.fetch('goodkind-mwan-steering:steering-group').fetch('hash-mode')
      raise Failure, 'calibrated hash mode differs from deployed mode' unless mode == calibration.mode

      eligible = @configured.fetch('interface').filter_map do |intent|
        steering = intent['goodkind-mwan-steering:steering']
        next unless steering

        actual = @tree.fetch('interface').find { |entry| entry.fetch('name') == intent.fetch('name') }
        next unless eligible?(actual, family)

        [intent.fetch('goodkind-mwan-steering:wan').fetch('name'), steering.fetch('tier'), steering.fetch('weight')]
      end
      raise Failure, "ipv#{family}: no eligible provider" if eligible.empty?

      tier = eligible.map { |entry| entry[1] }.min
      weights = eligible.select { |entry| entry[1] == tier }.to_h { |entry| [entry[0], entry[2]] }
      raise Failure, "ipv#{family}: calibrated weights or provider tier differs" unless weights == calibration.weights

      calibration
    end

    private

    def eligible?(actual, family)
      return false unless actual

      health = actual.fetch('goodkind-mwan-steering:steering').fetch('state').fetch('health')
      family_state = actual.fetch("ietf-ip:ipv#{family}", {})
      routing = family_state.fetch('goodkind-mwan-steering:ownership-family-state', {}).fetch('routing', 'unknown')
      translation = family_state.fetch('goodkind-mwan-steering:translation', {}).fetch('state', {}).fetch('ready', false)
      health == 'healthy' && routing == 'ready' && translation == true
    end
  end
  Request = Data.define(:id, :family, :source, :destination, :port, :started, :finished)
  Packet = Data.define(:time, :source, :destination, :flags, :sequence, :acknowledgment)

  # Each cohort opens fresh source-bound HTTP connections.
  class Requests
    def initialize(remote, plan)
      @remote = remote
      @plan = plan
    end

    def cohort(client, endpoint)
      Array.new(endpoint.count) do |index|
        id = "#{client.target.id}-ipv#{endpoint.family}-#{index}"
        request(client.target, endpoint, client.addresses.fetch(endpoint.family.to_s), id)
      end
    end

    def request(target, endpoint, source, id)
      uri = URI(endpoint.url)
      resolve_address = endpoint.family == 6 ? "[#{endpoint.address}]" : endpoint.address
      command = ['curl', "-#{endpoint.family}", '--interface', source, '--silent', '--show-error', '--fail', '--noproxy', '*', '--http1.1',
                 '--connect-timeout', @plan.timeout_seconds.to_s, '--max-time', (@plan.timeout_seconds - 1).to_s,
                 '--header', 'Connection: close', '--header', "X-Mwan-Acceptance: #{id}", '--resolve', "#{uri.host}:#{endpoint.port}:#{resolve_address}",
                 '--output', '/dev/null', '--write-out', CURL_JSON_FORMAT, endpoint.url]
      started = gateway_time("#{id}-start")
      response = @remote.json(target, command, id)
      raise Failure, "#{id}: HTTP status #{response.fetch('http_code')}" unless response.fetch('http_code') == 200
      raise Failure, "#{id}: endpoint differs" unless IPAddr.new(response.fetch('remote_ip')) == IPAddr.new(endpoint.address) && response.fetch('remote_port') == endpoint.port
      raise Failure, "#{id}: source differs" unless IPAddr.new(response.fetch('local_ip')) == IPAddr.new(source)

      Request.new(id: id, family: endpoint.family, source: "#{response.fetch('local_ip')}.#{response.fetch('local_port')}",
                  destination: "#{response.fetch('remote_ip')}.#{response.fetch('remote_port')}", port: endpoint.port, started: started, finished: gateway_time("#{id}-finish"))
    end

    private

    def gateway_time(label)
      Time.iso8601(@remote.read(@plan.gateway, ['date', '--iso-8601=ns'], label).strip.tr(',', '.')).to_f
    end
  end

  # TCP SYN sequences correlate traffic across translation boundaries.
  class Packets
    EXPRESSION = /\A(?<time>\d+\.\d+) IP6? (?<source>\S+) > (?<destination>\S+): Flags \[(?<flags>[^\]]+)\],(?: seq (?<sequence>\d+)(?::\d+)?,)?(?: ack (?<acknowledgment>\d+),)?/

    def initialize(captures)
      @captures = captures
    end

    def read(observer)
      @captures.packets(observer).lines.filter_map do |line|
        match = EXPRESSION.match(line)
        next unless match

        Packet.new(time: match[:time].to_f, source: match[:source], destination: match[:destination], flags: match[:flags],
                   sequence: match[:sequence]&.to_i, acknowledgment: match[:acknowledgment]&.to_i)
      end
    end

    def attribute(requests, transit, provider_packets, simulator_packets)
      requests.map do |request|
        syns = transit.select { |packet| packet.flags == 'S' && packet.destination == request.destination && packet.time >= request.started && packet.time <= request.finished }
        sequences = syns.map(&:sequence).uniq
        raise Failure, "#{request.id}: ambiguous or missing transit SYN" unless sequences.size == 1 && sequences[0]

        sequence = sequences[0]
        providers = provider_packets.keys.select do |provider|
          exchange?(provider_packets.fetch(provider), request.destination, sequence) && exchange?(simulator_packets.fetch(provider), request.destination, sequence)
        end
        raise Failure, "#{request.id}: ambiguous or missing provider request/reply" unless providers.size == 1 && exchange?(transit, request.destination, sequence)

        [request, providers[0], syns[0].source]
      end
    end

    def verify_counts(attributed, calibration)
      raise Failure, 'sample count differs from reviewed calibration' unless attributed.size == calibration.count

      counts = attributed.map { |entry| entry[1] }.tally
      raise Failure, 'packets selected an ineligible provider' unless (counts.keys - calibration.weights.keys).empty?

      calibration.bounds.each do |provider, bounds|
        count = counts.fetch(provider, 0)
        raise Failure, "#{provider}: count #{count} outside #{bounds}" unless count.between?(*bounds)
      end
      return counts if calibration.mode == 'random'

      verify_sticky(attributed, calibration)
      counts
    end

    def verify_sticky(attributed, calibration)
      groups = attributed.group_by do |request, _provider, source|
        address = source.rpartition('.')[0]
        calibration.mode == 'source' ? address : [address, request.destination.rpartition('.')[0]]
      end
      raise Failure, 'effective hash-key population differs from calibration' unless groups.size == calibration.effective_keys
      raise Failure, 'one sticky hash key selected multiple providers' unless groups.values.all? { |entries| entries.map { |entry| entry[1] }.uniq.size == 1 }
    end

    def verify_mapping(mapping, response, transit, provider, simulator)
      source = "#{response.fetch('local_ip')}.#{response.fetch('local_port')}"
      external = "#{mapping.external}.#{mapping.port}"
      internal = "#{mapping.internal}.#{mapping.port}"
      sequences = simulator.select { |packet| packet.flags == 'S' && packet.source == source && packet.destination == external }.map(&:sequence).uniq
      raise Failure, "#{mapping.provider}: mapping ingress SYN missing or ambiguous" unless sequences.size == 1 && sequences[0]

      sequence = sequences[0]
      verified = exchange?(simulator, external, sequence) && exchange?(provider, external, sequence) && exchange?(transit, internal, sequence)
      raise Failure, "#{mapping.provider}: translated mapping request/reply missing" unless verified

      { sequence: sequence, source: source, external: external, internal: internal }
    end

    private

    def exchange?(packets, destination, sequence)
      requests = packets.select { |packet| packet.flags == 'S' && packet.destination == destination && packet.sequence == sequence }
      requests.any? do |request|
        packets.any? { |reply| reply.flags == 'S.' && reply.source == destination && reply.destination == request.source && reply.acknowledgment == sequence + 1 }
      end
    end
  end

  # Mapping probes require served configuration and matching response content.
  class MappingProbes
    def initialize(remote, plan, product)
      @remote = remote
      @plan = plan
      @product = product
    end

    def verify(mapping)
      provider = @plan.providers.find { |entry| entry.id == mapping.provider }
      raise Failure, "mapping provider #{mapping.provider} missing" unless provider

      verify_served(mapping, provider)
      verify_route(mapping, provider)
      request(mapping, provider)
    end

    private

    def verify_served(mapping, provider)
      tree = @product.snapshot.fetch('ietf-interfaces:interfaces')
      member = tree.fetch('interface').find { |entry| entry.fetch('name') == provider.interface }
      translation = member.fetch("ietf-ip:ipv#{mapping.family}").fetch('goodkind-mwan-steering:translation')
      configured = translation.fetch('static-mapping').any? { |entry| entry.fetch('external') == mapping.external && entry.fetch('internal') == mapping.internal }
      raise Failure, "#{provider.id}: mapping is not served" unless configured && translation.fetch('state').fetch('ready')
    end

    def verify_route(mapping, provider)
      route = @remote.json(provider.target, ['ip', "-#{mapping.family}", '-j', 'route', 'get', mapping.external, 'from', mapping.source], "#{provider.id}-mapping-route")
      raise Failure, "#{provider.id}: mapping probe uses alternate uplink" unless route.size == 1 && route[0].fetch('dev') == provider.capture_interface
    end

    def request(mapping, provider)
      address = mapping.family == 6 ? "[#{mapping.external}]" : mapping.external
      command = ['curl', "-#{mapping.family}", '--interface', mapping.source, '--noproxy', '*', '--silent', '--show-error', '--fail', '--http1.1',
                 '--header', 'Connection: close', '--max-time', (@plan.timeout_seconds - 1).to_s, '--write-out', "\nMWAN-RESULT:#{CURL_JSON_FORMAT}\n",
                 "http://#{address}:#{mapping.port}#{mapping.path}"]
      output = @remote.read(provider.target, command, "#{provider.id}-mapping-http")
      body, separator, metadata = output.rpartition("\nMWAN-RESULT:")
      raise Failure, "#{provider.id}: mapping response metadata absent" if separator.empty?

      response = JSON.parse(metadata)
      raise Failure, "#{provider.id}: mapping response failed" unless response.fetch('http_code') == 200 && Digest::SHA256.hexdigest(body) == mapping.response_sha256

      response
    end
  end

  # Persistent failure records must remain readable after an observed restart.
  class HistoryObserver
    def initialize(remote, plan, product, processes, directory)
      @remote = remote
      @plan = plan
      @product = product
      @processes = processes
      @directory = directory
    end

    def verify
      before_pid = pid
      raise Failure, 'product service has no running main process' unless before_pid.positive?

      started = Time.iso8601(@remote.read(@plan.gateway, ['date', '--iso-8601=ns'], 'history-clock').strip.tr(',', '.'))
      deadline = @processes.monotonic + @plan.history.seconds
      records = []
      restarted = false
      File.write(File.join(@directory, 'phase.json'), JSON.generate({ phase: 'observing-failure-and-restart', started: started.iso8601(9), initial_pid: before_pid }))
      loop do
        raise Interrupted, 'acceptance interrupted' if @processes.interrupted

        current = history
        records |= current.select { |record| relevant?(record, started) }
        @product.snapshot
        current_pid = pid
        restarted ||= current_pid.positive? && current_pid != before_pid
        if restarted && records.any?
          missing = records.reject { |record| current.include?(record) }
          raise Failure, 'product transition records disappeared after restart' unless missing.empty?

          return { restart_observed: true, retained_transitions: records }
        end
        raise Failure, 'product restart and matching persistent failure history were not observed' if @processes.monotonic >= deadline

        sleep 0.2
      end
    end

    private

    def pid
      @remote.read(@plan.gateway, ['systemctl', 'show', @plan.history.service, '--property=MainPID', '--value'], 'product-main-pid').strip.to_i
    end

    def history
      attempts = 0
      begin
        read_history
      rescue Failure => e
        raise unless e.message.include?('product-history-records failed (1): cat:') && e.message.include?('No such file or directory')
        raise if attempts.positive?

        attempts += 1
        retry
      end
    end

    def read_history
      settings = @plan.history
      paths = @remote.read(@plan.gateway, ['find', settings.directory, '-maxdepth', '1', '-type', 'f', '-name', settings.pattern], 'product-history-paths').lines.map(&:strip).sort
      raise Failure, 'product history files missing' if paths.empty?

      paths.flat_map do |path|
        output = @remote.read(@plan.gateway, ['cat', path], 'product-history-records')
        output.lines.filter_map do |line|
          next if line.strip.empty?

          JSON.parse(line)
        end
      end
    rescue JSON::ParserError => e
      raise Failure, "product history is not readable JSON: #{e.message}"
    end

    def relevant?(record, started)
      transition = record['transition']
      return false unless transition
      return false unless record['connection_id'] == @plan.history.connection
      return false unless %w[not-ready failed].include?(transition.fetch('Current'))

      transition.fetch('Family') == @plan.history.family && transition.fetch('Dependency') == @plan.history.dependency &&
        transition.fetch('Reason') == @plan.history.reason && Time.iso8601(transition.fetch('At')) >= started
    end
  end

  # Acceptance and observer cleanup failures determine the command exit status.
  class Engine
    IPV4_ATTRIBUTION = 'Serialized cohorts match TCP SYN sequence and destination after OPNsense NAT. ' \
                       'Packet evidence does not prove each post-NAT guest source identity.'
    def initialize(plan, directory)
      @plan = plan
      @directory = directory
      @processes = Processes.new(directory)
      @remote = Remote.new(plan, @processes)
      @captures = nil
      @failure = nil
      @cleanup_errors = []
      @results = {}
    end

    def run
      previous_int = Signal.trap('INT') { @processes.interrupt }
      previous_term = Signal.trap('TERM') { @processes.interrupt }
      status = 0
      begin
        execute
      rescue Interrupted => e
        @failure = e.message
        status = 130
      rescue Failure, KeyError, JSON::ParserError, ArgumentError, SystemCallError => e
        @failure = e.message
        status = 1
      ensure
        cleanup
        status = 1 if status.zero? && !@cleanup_errors.empty?
        File.write(File.join(@directory, 'result.json'), JSON.pretty_generate({ failure: @failure, cleanup_errors: @cleanup_errors, results: @results, status: status,
                                                                                ipv4_attribution: IPV4_ATTRIBUTION }))
        Signal.trap('INT', previous_int) if previous_int
        Signal.trap('TERM', previous_term) if previous_term
      end
      status
    end

    private

    def execute
      product = Product.new(@remote, @plan)
      preflight = ClientPreflight.new(@remote, @plan)
      @plan.targets.each_value { |target| @remote.identity(target) }
      @plan.clients.each { |client| preflight.verify(client) }
      tree, configured = product.verify
      policy = Policy.new(@plan, tree, configured)
      @plan.endpoints.each { |endpoint| @results["ipv#{endpoint.family}"] = family(endpoint, policy.calibration(endpoint.family)) }
      @plan.mappings.each { |mapping| mapping_phase(mapping, product) }
      @results['history'] = HistoryObserver.new(@remote, @plan, product, @processes, @directory).verify
    end

    def family(endpoint, calibration)
      attributed = []
      @plan.clients.each do |client|
        captures(endpoint.port)
        requests = Requests.new(@remote, @plan).cohort(client, endpoint)
        @captures.stop
        parser = Packets.new(@captures)
        transit = parser.read(@captures.observers.find { |observer| observer.label == 'transit' })
        providers = @plan.providers.to_h { |provider| [provider.id, parser.read(@captures.observers.find { |observer| observer.label == "gateway-#{provider.id}" })] }
        simulators = @plan.providers.to_h { |provider| [provider.id, parser.read(@captures.observers.find { |observer| observer.label == "simulator-#{provider.id}" })] }
        attributed.concat(parser.attribute(requests, transit, providers, simulators))
        @captures = nil
      end
      Packets.new(nil).verify_counts(attributed, calibration)
    end

    def captures(port)
      @captures = Captures.new(@remote, @processes, @plan, SecureRandom.hex(6))
      @captures.start(@plan.gateway, @plan.transit, 'transit', port)
      File.write(File.join(@directory, 'capture-phase.json'), JSON.generate({ phase: 'capturing', observer: @captures.observers.first.unit }))
      @plan.providers.each do |provider|
        @captures.start(@plan.gateway, provider.interface, "gateway-#{provider.id}", port)
        @captures.start(provider.target, provider.capture_interface, "simulator-#{provider.id}", port)
      end
    end

    def mapping_phase(mapping, product)
      captures(mapping.port)
      response = MappingProbes.new(@remote, @plan, product).verify(mapping)
      @captures.stop
      parser = Packets.new(@captures)
      transit = parser.read(@captures.observers.find { |observer| observer.label == 'transit' })
      provider = parser.read(@captures.observers.find { |observer| observer.label == "gateway-#{mapping.provider}" })
      simulator = parser.read(@captures.observers.find { |observer| observer.label == "simulator-#{mapping.provider}" })
      @results["mapping-#{mapping.provider}-ipv#{mapping.family}"] = parser.verify_mapping(mapping, response, transit, provider, simulator)
      @captures = nil
    end

    def cleanup
      @captures&.stop
    rescue Failure, SystemCallError => e
      @cleanup_errors.push(e.message)
      @failure ||= 'observer cleanup failed'
    ensure
      @processes.cleanup
    end
  end
end
