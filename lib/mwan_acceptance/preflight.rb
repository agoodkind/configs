# frozen_string_literal: true

module MwanAcceptance
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
end
