# frozen_string_literal: true

module MwanAcceptance
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
      Packets.verify_counts(attributed, calibration)
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
