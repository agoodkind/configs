# frozen_string_literal: true

module MwanAcceptance
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
end
