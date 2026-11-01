terraform {
  required_providers {
    pveguest = {
      source  = "tofu.home.arpa/agoodkind/pveguest"
      version = "0.1.1"
    }
  }
}

variable "node" {
  description = "node is the hypervisor key in the pveguest provider nodes map. The module sets node on every pveguest resource."
  type        = string
}

variable "vmid" {
  description = "vmid is the Proxmox ID of the container that the module configures."
  type        = number
}

variable "repository_root" {
  description = "repository_root is the absolute path of the repository checkout. The module reads files below ansible/inventory/group_vars and testbed/routing-simulators in that checkout."
  type        = string
}

variable "name" {
  description = "name selects the testbed_routing_nodes entry to configure, such as tunnel_vps_vps. The module reads only the scenario file that the entry selects in its scenario field."
  type        = string
}

variable "guest_key" {
  description = "guest_key is the guest's service_mapping key. A resource precondition fails the plan when the testbed_routing_nodes entry that name selects uses a different service or vmid."
  type        = string
}

variable "service_mapping" {
  description = "service_mapping is the decoded service mapping. The module uses it to resolve scenario placeholders and read routing interfaces. A placeholder that references a missing guest or field fails the plan with an invalid index error."
  type        = any
}

variable "routing_inventory" {
  description = "routing_inventory is the decoded service_mapping.yml file. The module reads testbed_routing_networks from it."
  type        = any
}

locals {
  kind = "lxc"

  routing_vars_dir = "${var.repository_root}/ansible/inventory/group_vars/testbed_routing_all"
  shared_files_dir = "${var.repository_root}/testbed/routing-simulators"

  # Scenario and connection files use {{ service_mapping.guest.field }} placeholders,
  # optionally followed by text such as /32. The pattern splits each value into
  # its dotted path and trailing text. The files do not use other template syntax.
  placeholder_pattern = "^\\{\\{ ([A-Za-z0-9_.]+) \\}\\}(.*)$"
  placeholder_scan    = "\"(\\{\\{[^\"]*)\""

  nodes              = yamldecode(file("${local.routing_vars_dir}/nodes.yml")).testbed_routing_nodes
  connection_sources = yamldecode(file("${local.routing_vars_dir}/connections.yml")).testbed_routing_connections
  home_prefix        = yamldecode(file("${local.routing_vars_dir}/prefixes.yml")).testbed_routing_home_prefix
  checks             = yamldecode(file("${local.routing_vars_dir}/checks.yml")).testbed_routing_checks
  deploy_vars        = yamldecode(file("${local.routing_vars_dir}/deploy.yml"))
  default_routes     = yamldecode(file("${local.routing_vars_dir}/contracts.yml")).routing_simulator_default_routes

  tracker_settings = local.deploy_vars.testbed_routing_home_route_tracker
  networks         = var.routing_inventory.testbed_routing_networks

  # Ansible keeps the file order of testbed_routing_networks. yamldecode sorts keys
  # and does not keep that order. Bridge names use vmbr followed by a number that
  # increases in file order. The module sorts networks by that number to reproduce
  # Ansible's file order. The render comparison against the Ansible templates detects
  # differences in that order.
  network_bridge_keys = sort([
    for network_name, network in local.networks :
    format("%06d:%s", tonumber(regex("^vmbr([0-9]+)$", network.bridge)[0]), network_name)
  ])
  network_order = [for bridge_key in local.network_bridge_keys : split(":", bridge_key)[1]]

  # The module reads the literal prefix from the
  # testbed_routing_management_unreachable_prefixes expression in prefixes.yml.
  # The module compares the full expression with the shape it implements.
  # A resource precondition fails the plan when prefixes.yml changes the rule.
  unreachable_expression = yamldecode(
    file("${local.routing_vars_dir}/prefixes.yml")
  ).testbed_routing_management_unreachable_prefixes
  extra_unreachable_prefix = regex(
    "^\\{\\{ \\[testbed_routing_home_prefix, '([^']+)'\\] ",
    local.unreachable_expression
  )[0]
  unreachable_expression_current = (
    local.unreachable_expression == "{{ [testbed_routing_home_prefix, '${local.extra_unreachable_prefix}'] + (testbed_routing_networks | dict2items | rejectattr('key', 'equalto', 'management') | map(attribute='value.ipv4_net') | reject('equalto', '') | list) }}"
  )

  node    = local.nodes[var.name]
  service = var.service_mapping[local.node.service]
}

locals {
  # Connection values reference only service_mapping. One lookup pass resolves them.
  # The lookup tables use each dotted path as a key. A missing path fails the plan
  # with an invalid index error.
  service_fields = merge([
    for guest, entry in var.service_mapping : {
      for field, value in entry : "service_mapping.${guest}.${field}" => value
      if can(tostring(value))
    }
  ]...)
  service_interface_fields = merge(flatten([
    for guest, entry in var.service_mapping : [
      for interface_key, interface in try(entry.routing_interfaces, {}) : {
        for field, value in interface : "service_mapping.${guest}.routing_interfaces.${interface_key}.${field}" => value
        if can(tostring(value))
      }
    ]
  ])...)
  service_values = merge(local.service_fields, local.service_interface_fields)

  connection_strings = distinct([
    for match in regexall(local.placeholder_scan, jsonencode(local.connection_sources)) : match[0]
  ])
  connection_paths = distinct([
    for text in local.connection_strings : regex(local.placeholder_pattern, text)[0]
  ])
  connection_values = {
    for path in local.connection_paths : path => local.service_values[path]
  }
  connection_resolved = {
    for text in local.connection_strings : text => "${local.connection_values[regex(local.placeholder_pattern, text)[0]]}${regex(local.placeholder_pattern, text)[1]}"
  }
  connections = {
    for id, connection in local.connection_sources : id => {
      for field, value in connection : field => lookup(local.connection_resolved, value, value)
    }
  }
}

locals {
  shared_scenario = {
    kind          = "shared"
    tunnels       = []
    routes        = []
    sessions      = []
    home_prefixes = []
  }

  # The module decodes only its node's scenario file. When the node's scenario field
  # is empty, the module uses an empty shared scenario and does not read a scenario file.
  scenario_documents = {
    for scenario_name in toset(compact([local.node.scenario])) : scenario_name => yamldecode(
      file("${local.routing_vars_dir}/scenario_${scenario_name}.yml")
    )["testbed_routing_scenario_${scenario_name}"]
  }
  scenario_source = try(local.scenario_documents[local.node.scenario], local.shared_scenario)

  network_values = merge([
    for network_name, network in local.networks : {
      for field, value in network : "testbed_routing_networks.${network_name}.${field}" => value
    }
  ]...)
  connection_field_values = merge([
    for connection_id, connection in local.connections : {
      for field, value in connection : "testbed_routing_connections.${connection_id}.${field}" => value
    }
  ]...)
  scenario_context = merge(
    local.service_values,
    local.network_values,
    local.connection_field_values,
    { testbed_routing_home_prefix = local.home_prefix },
  )
  scenario_strings = distinct([
    for match in regexall(local.placeholder_scan, jsonencode(local.scenario_source)) : match[0]
  ])
  scenario_paths = distinct([
    for text in local.scenario_strings : regex(local.placeholder_pattern, text)[0]
  ])
  scenario_values = {
    for path in local.scenario_paths : path => local.scenario_context[path]
  }
  scenario_resolved = {
    for text in local.scenario_strings : text => "${local.scenario_values[regex(local.placeholder_pattern, text)[0]]}${regex(local.placeholder_pattern, text)[1]}"
  }

  scenario = {
    kind = local.scenario_source.kind
    home_prefixes = [
      for prefix in local.scenario_source.home_prefixes : lookup(local.scenario_resolved, prefix, prefix)
    ]
    tunnels = [
      for tunnel in local.scenario_source.tunnels : {
        id                  = tunnel.id
        protocol            = tunnel.protocol
        mtu                 = tunnel.mtu
        outer_ttl           = tunnel.outer_ttl
        inner_prefix_length = tunnel.inner_prefix_length
        local = {
          node       = tunnel.local.node
          outer_ipv4 = lookup(local.scenario_resolved, tunnel.local.outer_ipv4, tunnel.local.outer_ipv4)
          inner_ipv6 = lookup(local.scenario_resolved, tunnel.local.inner_ipv6, tunnel.local.inner_ipv6)
        }
        remote = {
          node       = tunnel.remote.node
          interface  = tunnel.remote.interface
          outer_ipv4 = lookup(local.scenario_resolved, tunnel.remote.outer_ipv4, tunnel.remote.outer_ipv4)
          inner_ipv6 = lookup(local.scenario_resolved, tunnel.remote.inner_ipv6, tunnel.remote.inner_ipv6)
        }
      }
    ]
    routes = [
      for route in local.scenario_source.routes : {
        node        = route.node
        destination = lookup(local.scenario_resolved, route.destination, route.destination)
        gateway     = lookup(local.scenario_resolved, route.gateway, route.gateway)
      }
    ]
    sessions = [
      for session in local.scenario_source.sessions : {
        id                = session.id
        multihop_ttl      = session.multihop_ttl
        keepalive_seconds = session.keepalive_seconds
        hold_seconds      = session.hold_seconds
        local = {
          node    = session.local.node
          asn     = session.local.asn
          address = lookup(local.scenario_resolved, session.local.address, session.local.address)
        }
        remote = {
          node    = session.remote.node
          asn     = session.remote.asn
          address = lookup(local.scenario_resolved, session.remote.address, session.remote.address)
        }
        policy = {
          import_prefixes = [
            for prefix in session.policy.import_prefixes : lookup(local.scenario_resolved, prefix, prefix)
          ]
          export_prefixes = [
            for prefix in session.policy.export_prefixes : lookup(local.scenario_resolved, prefix, prefix)
          ]
        }
      }
    ]
  }
}

locals {
  role            = local.node.role
  bgp             = local.node.router_id != ""
  forwarding      = contains(["transport", "tunnel_endpoint", "provider_router", "vps", "upstream_router"], local.role)
  transfer_server = local.role == "remote_client"
  hostname        = split(".", local.service.hostname)[0]

  all_interfaces       = values(local.service.routing_interfaces)
  management_interface = one([for interface in local.all_interfaces : interface.name if interface.network == "management"])
  interface_names      = sort([for interface in local.all_interfaces : interface.name if interface.network != "management"])
  interfaces = [
    for interface_name in local.interface_names : one([
      for interface in local.all_interfaces : interface if interface.name == interface_name
    ])
  ]

  service_interface = local.node.service_interface == "" ? "" : local.service.routing_interfaces[local.node.service_interface].name

  packages = toset(concat(
    ["nftables", "tcpdump", "iperf3", "iputils-ping"],
    local.bgp ? ["frr"] : [],
  ))

  provider_link_addresses = [for connection in values(local.connections) : connection.simulator_link_ipv4]
  provider_links = [
    for interface in local.all_interfaces : interface
    if local.role == "transport" && contains(local.provider_link_addresses, interface.ipv4)
  ]
  uplink_interfaces = [for interface in local.provider_links : interface.name]
  uplink_sources    = [for interface in local.provider_links : local.networks[interface.network].ipv4_net]

  tunnel_octets = {
    for tunnel in local.scenario.tunnels : tunnel.id => [
      for part in concat(split(".", tunnel.remote.outer_ipv4), split(".", tunnel.local.outer_ipv4)) : tonumber(part)
    ]
  }
  tunnels = [
    for tunnel in local.scenario.tunnels : merge(tunnel, {
      link_local_ipv6 = format(
        "fe80::%x:%x:%x:%x",
        local.tunnel_octets[tunnel.id][0] * 256 + local.tunnel_octets[tunnel.id][1],
        local.tunnel_octets[tunnel.id][2] * 256 + local.tunnel_octets[tunnel.id][3],
        local.tunnel_octets[tunnel.id][4] * 256 + local.tunnel_octets[tunnel.id][5],
        local.tunnel_octets[tunnel.id][6] * 256 + local.tunnel_octets[tunnel.id][7],
      )
    })
    if tunnel.remote.node == var.name
  ]

  tracked_destinations = [
    for prefix in local.scenario.home_prefixes : prefix
    if local.scenario.kind == "tunnel_configured_home" && local.role == "vps"
  ]
  node_routes   = [for route in local.scenario.routes : route if route.node == var.name]
  static_routes = [for route in local.node_routes : route if !contains(local.tracked_destinations, route.destination)]
  tracked_routes = [
    for route in local.node_routes : {
      destination = route.destination
      gateway     = route.gateway
      interface = try(reverse([
        for tunnel in local.tunnels : tunnel.remote.interface if tunnel.local.inner_ipv6 == route.gateway
      ])[0], "")
    }
    if contains(local.tracked_destinations, route.destination)
  ]

  sessions = concat(
    [for session in local.scenario.sessions : session if session.local.node == var.name],
    [for session in local.scenario.sessions : session if session.remote.node == var.name],
  )

  gateway_candidates = {
    for gateway in distinct([for route in local.static_routes : route.gateway]) : gateway => distinct(flatten([
      for entry in values(var.service_mapping) : [
        for candidate in values(try(entry.routing_interfaces, {})) : candidate.network
        if try(candidate.ipv4, "") == gateway || try(candidate.ipv6, "") == gateway
      ]
    ]))
  }
  gateway_networks = {
    for gateway, candidates in local.gateway_candidates : gateway => (
      length(candidates) == 0 ? "" : candidates[length(candidates) - 1]
    )
  }

  unreachable_prefixes = concat(
    [local.home_prefix, local.extra_unreachable_prefix],
    [
      for network_name in local.network_order : local.networks[network_name].ipv4_net
      if network_name != "management" && local.networks[network_name].ipv4_net != ""
    ],
  )
}

locals {
  bgp_peers = [
    for session in local.sessions : (
      session.local.node == var.name
      ? {
        session = session
        own     = session.local
        remote  = session.remote
        imports = session.policy.import_prefixes
        exports = session.policy.export_prefixes
      }
      : {
        session = session
        own     = session.remote
        remote  = session.local
        imports = session.policy.export_prefixes
        exports = session.policy.import_prefixes
      }
    )
  ]
  bgp_asn       = length(local.bgp_peers) == 0 ? 0 : local.bgp_peers[length(local.bgp_peers) - 1].own.asn
  bgp_learned   = flatten([for peer in local.bgp_peers : peer.imports])
  bgp_connected = [for interface in local.interfaces : local.networks[interface.network].ipv6_net if local.networks[interface.network].ipv6_net != ""]
  bgp_unlearned_exports = [
    for peer in local.bgp_peers : [
      for prefix in peer.exports : prefix if !contains(local.bgp_learned, prefix)
    ]
  ]
  tracked_route_destinations = [for route in local.tracked_routes : route.destination]
  bgp_networks = distinct(flatten([
    for exports in local.bgp_unlearned_exports : [
      for prefix in exports : prefix if contains(local.bgp_connected, prefix)
    ]
  ]))
  bgp_tracked = flatten([
    for exports in local.bgp_unlearned_exports : [
      for prefix in exports : [
        for route in local.tracked_routes : [
          for tunnel in local.tunnels : {
            prefix    = prefix
            interface = tunnel.remote.interface
          }
          if tunnel.local.inner_ipv6 == route.gateway
        ]
        if route.destination == prefix
      ]
      if !contains(local.bgp_connected, prefix) && contains(local.tracked_route_destinations, prefix)
    ]
  ])
  bgp_default_origin = anytrue(flatten([
    for exports in local.bgp_unlearned_exports : [
      for prefix in exports : (
        contains(local.default_routes, prefix)
        && !contains(local.bgp_connected, prefix)
        && !contains(local.tracked_route_destinations, prefix)
      )
    ]
  ]))
  gateway_peer = anytrue([for peer in local.bgp_peers : peer.remote.node == "mwan"])
}

locals {
  test_interface_names = concat(
    [for interface in local.interfaces : interface.name],
    [for tunnel in local.tunnels : tunnel.remote.interface],
  )
  test_interface_list = join(", ", [for interface_name in local.test_interface_names : jsonencode(interface_name)])

  nftables_content = templatefile("${path.module}/files/nftables.conf.tftpl", {
    name                 = var.name
    management_interface = local.management_interface
    uplink_sources       = local.uplink_sources
    uplink_interface_list = join(", ", [
      for interface_name in local.uplink_interfaces : jsonencode(interface_name)
    ])
    uplink_source_list  = join(", ", local.uplink_sources)
    test_interface_list = local.test_interface_list
    tunnels             = local.tunnels
    sessions            = local.sessions
    transfer_server     = local.transfer_server
    transfer_port       = local.checks.transfer_port
    forwarding          = local.forwarding
  })

  sysctl_content = templatefile("${path.module}/files/sysctl.conf.tftpl", {
    forwarding_value = local.forwarding ? 1 : 0
  })

  frr_content = local.bgp ? templatefile("${path.module}/files/frr.conf.tftpl", {
    hostname          = local.hostname
    peers             = local.bgp_peers
    tracked           = local.bgp_tracked
    default_origin    = local.bgp_default_origin
    default_routes    = local.default_routes
    service_interface = local.service_interface
    asn               = local.bgp_asn
    router_id         = local.node.router_id
    gateway_peer      = local.gateway_peer
    stalepath_seconds = local.deploy_vars.testbed_routing_graceful_restart_stalepath_seconds
    networks          = local.bgp_networks
  }) : ""

  interface_files = {
    for interface in local.interfaces : "/etc/systemd/network/50-rsim-${interface.name}.network" => templatefile(
      "${path.module}/files/interface.network.tftpl",
      {
        name = interface.name
        addresses = concat(
          interface.ipv4 == "" ? [] : [
            "${interface.ipv4}/${split("/", local.networks[interface.network].ipv4_net)[1]}"
          ],
          interface.ipv6 == "" ? [] : [
            "${interface.ipv6}/${split("/", local.networks[interface.network].ipv6_net)[1]}"
          ],
        )
        routes = [
          for route in local.static_routes : route
          if local.gateway_networks[route.gateway] == interface.network
        ]
        unreachable_prefixes      = interface.name == local.interfaces[0].name ? local.unreachable_prefixes : []
        unreachable_rule_priority = local.deploy_vars.testbed_routing_unreachable_rule_priority
        unreachable_metric        = local.deploy_vars.testbed_routing_unreachable_metric
      }
    )
  }
  netdev_files = {
    for tunnel in local.tunnels : "/etc/systemd/network/40-rsim-${tunnel.remote.interface}.netdev" => templatefile(
      "${path.module}/files/tunnel.netdev.tftpl",
      { tunnel = tunnel }
    )
  }
  tunnel_network_files = {
    for tunnel in local.tunnels : "/etc/systemd/network/40-rsim-${tunnel.remote.interface}.network" => templatefile(
      "${path.module}/files/tunnel.network.tftpl",
      {
        tunnel          = tunnel
        link_local_ipv6 = tunnel.link_local_ipv6
        routes = [
          for route in local.static_routes : route if route.gateway == tunnel.local.inner_ipv6
        ]
      }
    )
  }
  network_files = merge(
    { "/etc/systemd/networkd.conf.d/routing-simulator.conf" = file("${local.shared_files_dir}/networkd.conf") },
    local.interface_files,
    local.netdev_files,
    local.tunnel_network_files,
  )

  tracker_units = {
    for index, route in local.tracked_routes : tostring(index) => templatefile(
      "${path.module}/files/home-route-tracker.service.tftpl",
      {
        route             = route
        interval_seconds  = local.tracker_settings.interval_seconds
        failure_threshold = local.tracker_settings.failure_threshold
        route_metric      = local.tracker_settings.route_metric
      }
    )
  }
}

# The pinned pveguest 0.1.1 resources cannot express these Ansible operations.
# Ansible keeps these operations until a pinned release supports them.
# Ansible stops and disables frr.service on a node without BGP when the unit exists.
# pveguest_systemd_unit fails when the unit file does not exist.
# Ansible deletes network files in /etc/systemd/network that the inventory
# no longer declares. pveguest_file cannot remove a file during apply.
# Ansible maintains the applied record at /var/lib/routing-simulator/applied.
# Ansible deletes the old candidate files /etc/nftables.conf.candidate and
# /etc/frr/frr.conf.candidate.
resource "pveguest_apt_packages" "routing" {
  node = var.node
  vmid = var.vmid
  kind = local.kind

  packages = local.packages

  lifecycle {
    precondition {
      condition     = local.node.service == var.guest_key && local.service.vmid == var.vmid
      error_message = "The testbed_routing_nodes entry ${var.name} must use the service ${var.guest_key} with the vmid ${var.vmid}."
    }

    precondition {
      condition     = local.unreachable_expression_current
      error_message = "The testbed_routing_management_unreachable_prefixes rule in prefixes.yml changed. Update the routingsim module to match the new rule."
    }
  }
}

# The Debian nftables package installs /etc/nftables.conf with mode 0755.
# Ansible copies new content over that file and preserves mode 0755.
# The validate command runs nft -c on the temporary file before the resource
# replaces /etc/nftables.conf.
resource "pveguest_file" "nftables" {
  node = var.node
  vmid = var.vmid
  kind = local.kind

  path     = "/etc/nftables.conf"
  content  = local.nftables_content
  mode     = "0755"
  validate = "nft -c -f %s"

  depends_on = [pveguest_apt_packages.routing]
}

resource "pveguest_systemd_unit" "nftables" {
  node = var.node
  vmid = var.vmid
  kind = local.kind

  name    = "nftables.service"
  enabled = true
  active  = true

  restart_on = {
    conf = pveguest_file.nftables.write_id
  }

  depends_on = [pveguest_apt_packages.routing]
}

resource "pveguest_file" "sysctl" {
  node = var.node
  vmid = var.vmid
  kind = local.kind

  path    = "/etc/sysctl.d/90-routing-simulator.conf"
  content = local.sysctl_content
}

resource "pveguest_systemd_unit" "sysctl" {
  node = var.node
  vmid = var.vmid
  kind = local.kind

  name    = "systemd-sysctl.service"
  enabled = true
  active  = true

  restart_on = {
    conf = pveguest_file.sysctl.write_id
  }
}

resource "pveguest_file" "network" {
  for_each = local.network_files

  node = var.node
  vmid = var.vmid
  kind = local.kind

  path    = each.key
  content = each.value
}

# Ansible runs networkctl reload. The provider can only restart the unit.
# A restart reads every network file again. The networkd.conf drop-in sets
# ManageForeignRoutes=no to preserve routes that other programs install across
# the restart.
resource "pveguest_systemd_unit" "networkd" {
  node = var.node
  vmid = var.vmid
  kind = local.kind

  name    = "systemd-networkd.service"
  enabled = true
  active  = true

  restart_on = {
    for path, network_file in pveguest_file.network : path => network_file.write_id
  }
}

resource "pveguest_file" "frr_conf" {
  for_each = local.bgp ? toset(["frr.conf"]) : toset([])

  node = var.node
  vmid = var.vmid
  kind = local.kind

  path     = "/etc/frr/frr.conf"
  content  = local.frr_content
  mode     = "0640"
  owner    = "frr"
  group    = "frr"
  validate = "vtysh -C -f %s"

  depends_on = [pveguest_apt_packages.routing]
}

# Ansible uses pct push --perms 0640 to create the daemons file with owner root
# and group root. This resource keeps the default owner and group.
# Ansible installs frr.conf with install -o frr -g frr -m 0640.
# The frr.conf resource uses owner frr and group frr.
resource "pveguest_file" "frr_daemons" {
  for_each = local.bgp ? toset(["daemons"]) : toset([])

  node = var.node
  vmid = var.vmid
  kind = local.kind

  path    = "/etc/frr/daemons"
  content = file("${path.module}/files/frr-daemons")
  mode    = "0640"

  depends_on = [pveguest_apt_packages.routing]
}

resource "pveguest_systemd_unit" "frr" {
  for_each = local.bgp ? toset(["frr.service"]) : toset([])

  node = var.node
  vmid = var.vmid
  kind = local.kind

  name    = each.key
  enabled = true
  active  = true

  restart_on = {
    conf    = pveguest_file.frr_conf["frr.conf"].write_id
    daemons = pveguest_file.frr_daemons["daemons"].write_id
  }

  # The units start after systemd-networkd.service. On a rebuilt guest, the tunnel
  # interfaces exist before FRR and the route tracker use them.
  depends_on = [pveguest_apt_packages.routing, pveguest_systemd_unit.networkd]
}

resource "pveguest_systemd_unit" "iperf3" {
  for_each = local.transfer_server ? toset(["iperf3.service"]) : toset([])

  node = var.node
  vmid = var.vmid
  kind = local.kind

  name    = each.key
  enabled = true
  active  = true

  depends_on = [pveguest_apt_packages.routing]
}

resource "pveguest_file" "tracker_script" {
  for_each = length(local.tracked_routes) > 0 ? toset(["script"]) : toset([])

  node = var.node
  vmid = var.vmid
  kind = local.kind

  path    = "/usr/local/sbin/routing-home-route-tracker"
  content = file("${local.shared_files_dir}/home-route-tracker.sh")
  mode    = "0755"

  depends_on = [pveguest_apt_packages.routing]
}

resource "pveguest_file" "tracker_unit" {
  for_each = local.tracker_units

  node = var.node
  vmid = var.vmid
  kind = local.kind

  path    = "/etc/systemd/system/routing-home-route-${each.key}.service"
  content = each.value
}

resource "pveguest_systemd_unit" "tracker" {
  for_each = local.tracker_units

  node = var.node
  vmid = var.vmid
  kind = local.kind

  name    = "routing-home-route-${each.key}.service"
  enabled = true
  active  = true

  restart_on = {
    script = pveguest_file.tracker_script["script"].write_id
    unit   = pveguest_file.tracker_unit[each.key].write_id
  }

  # The units start after systemd-networkd.service. On a rebuilt guest, the tunnel
  # interfaces exist before FRR and the route tracker use them.
  depends_on = [pveguest_apt_packages.routing, pveguest_systemd_unit.networkd]
}
