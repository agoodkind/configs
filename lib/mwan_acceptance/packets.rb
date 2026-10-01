# frozen_string_literal: true

module MwanAcceptance
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

    def self.verify_counts(attributed, calibration)
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

    def self.verify_sticky(attributed, calibration)
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

  # Mapping probes require deployed intent, current readiness and matching response content.
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
      intent = @remote.json(@plan.gateway, ['cat', @plan.network_json], 'mapping-intent').fetch('ietf-interfaces:interfaces')
      configured_member = intent.fetch('interface').find { |entry| entry.fetch('name') == provider.interface }
      mappings = configured_member.fetch("ietf-ip:ipv#{mapping.family}").fetch('goodkind-mwan-steering:translation').fetch('static-mapping')
      configured = mappings.any? { |entry| entry.fetch('external') == mapping.external && entry.fetch('internal') == mapping.internal }
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
end
