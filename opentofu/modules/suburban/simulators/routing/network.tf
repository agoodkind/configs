# Add one entry for each guest key in guest_keys.
# Every role except remote_client forwards traffic between test interfaces.
# Each remote_client guest runs iperf3.service.
# masquerade_interfaces lists routing_interfaces keys. The guest masquerades the IPv4
# network of each listed interface on the management interface.
# Each tunnel ends on the guest. outer_ipv4 and inner_ipv6 are guest addresses.
# peer_outer_ipv4 and peer_inner_ipv6 are addresses of the far end.
# The module writes each static route to the interface file for the network containing
# its gateway. A route using a tunnel's peer_inner_ipv6 as its gateway uses that
# tunnel file.
variable "network" {
  type = map(object({
    role                  = string
    masquerade_interfaces = optional(list(string), [])
    tunnels = optional(list(object({
      interface           = string
      mtu                 = number
      outer_ttl           = number
      inner_prefix_length = number
      outer_ipv4          = string
      peer_outer_ipv4     = string
      inner_ipv6          = string
      peer_inner_ipv6     = string
    })), [])
    routes = optional(list(object({
      destination = string
      gateway     = string
    })), [])
  }))

  validation {
    condition     = toset(keys(var.network)) == toset(var.guest_keys)
    error_message = "network must contain exactly one entry for each guest key in guest_keys."
  }

  validation {
    condition = alltrue([
      for guest in values(var.network) :
      contains(["transport", "tunnel_endpoint", "provider_router", "vps", "upstream_router", "remote_client"], guest.role)
    ])
    error_message = "Each network role must be transport, tunnel_endpoint, provider_router, vps, upstream_router, or remote_client."
  }
}

# The module writes these prefixes as unreachable routes and rules in each
# guest's first non-management interface file, ordered by interface name.
variable "unreachable_prefixes" { type = list(string) }

locals {
  network_packages = toset(["iperf3", "iputils-ping", "nftables", "tcpdump"])

  # These roles enable IPv4 and IPv6 forwarding and accept forwarded traffic
  # between test interfaces and tunnels. Other roles disable forwarding.
  network_forwarding_roles = toset(["transport", "tunnel_endpoint", "provider_router", "vps", "upstream_router"])
  network_transfer_role    = "remote_client"
  network_transfer_port    = 5201
  network_tunnel_kind      = "sit"

  network_unreachable_rule_priority = 32800
  network_unreachable_metric        = 65000

  network_unit_directory = "/etc/systemd/network"

  network_guests = {
    for name, guest in local.routing_guests : name => {
      node            = guest.node
      vmid            = guest.vmid
      forwarding      = contains(local.network_forwarding_roles, var.network[name].role)
      transfer_server = var.network[name].role == local.network_transfer_role
      management_name = local.routing_management_interfaces[name].name
      interface_names = sort([
        for interface in values(guest.routing_interfaces) : interface.name
        if interface.network != local.routing_management_network
      ])
      interfaces = {
        for interface in values(guest.routing_interfaces) : interface.name => interface
        if interface.network != local.routing_management_network
      }
      masquerade = [for key in var.network[name].masquerade_interfaces : guest.routing_interfaces[key]]
      tunnels    = var.network[name].tunnels
      routes     = var.network[name].routes
      bgp_peers  = contains(keys(var.frr), name) ? [for peer in var.frr[name].peers : peer.address] : []
    }
  }

  # The module writes one 50-rsim-<interface>.network file per non-management
  # interface and assigns routes by the interface network containing the gateway.
  network_interface_files = merge([
    for name, guest in local.network_guests : {
      for index, interface_name in guest.interface_names : "${name}/${interface_name}" => {
        guest     = name
        interface = guest.interfaces[interface_name]
        first     = index == 0
        route_indexes = [
          for route_index, route in guest.routes : route_index
          if try(cidrcontains(local.network_interface_prefixes[name][interface_name][strcontains(route.gateway, ":") ? "ipv6" : "ipv4"], route.gateway), false)
        ]
      }
    }
  ]...)

  network_interface_prefixes = {
    for name, guest in local.network_guests : name => {
      for interface_name, interface in guest.interfaces : interface_name => {
        ipv4 = local.routing_networks[interface.network].ipv4_net
        ipv6 = local.routing_networks[interface.network].ipv6_net
      }
    }
  }

  # systemd-networkd sets addrgenmode none on SIT devices. Each device lacks an IPv6
  # link-local address, which FRR needs to set the next hop of a BGP session over a
  # tunnel.
  # Each tunnel network file adds a link-local address with an interface identifier
  # formed from the guest and peer outer IPv4 addresses. Each outer address pair must
  # be unique.
  network_tunnel_files = merge([
    for name, guest in local.network_guests : {
      for tunnel in guest.tunnels : "${name}/${tunnel.interface}" => {
        guest  = name
        tunnel = tunnel
        octets = [for part in concat(split(".", tunnel.outer_ipv4), split(".", tunnel.peer_outer_ipv4)) : tonumber(part)]
        route_indexes = [
          for route_index, route in guest.routes : route_index
          if route.gateway == tunnel.peer_inner_ipv6
        ]
      }
    }
  ]...)

  # Count the interface and tunnel files that receive each route index for each guest.
  # Each index identifies a separate route, even when two routes have equal values.
  # The networkd_conf precondition requires exactly one file for each route.
  network_placed_route_indexes = {
    for name, guest in local.network_guests : name => concat(
      flatten([for file in values(local.network_interface_files) : file.route_indexes if file.guest == name]),
      flatten([for file in values(local.network_tunnel_files) : file.route_indexes if file.guest == name]),
    )
  }

  network_route_placement_counts = {
    for name, guest in local.network_guests : name => [
      for route_index in range(length(guest.routes)) : length([
        for placed_index in local.network_placed_route_indexes[name] : placed_index
        if placed_index == route_index
      ])
    ]
  }
}

resource "pveguest_apt_packages" "network" {
  for_each = local.network_guests

  node = each.value.node
  vmid = each.value.vmid
  kind = local.routing_guest_kind

  packages = local.network_packages

  depends_on = [proxmox_virtual_environment_container.routing_simulator]
}

resource "pveguest_file" "networkd_conf" {
  for_each = local.network_guests

  node = each.value.node
  vmid = each.value.vmid
  kind = local.routing_guest_kind

  path    = "/etc/systemd/networkd.conf.d/routing-simulator.conf"
  content = file("${path.module}/files/networkd.conf")

  depends_on = [proxmox_virtual_environment_container.routing_simulator]

  lifecycle {
    precondition {
      condition     = alltrue([for count in local.network_route_placement_counts[each.key] : count == 1])
      error_message = "Each route of guest ${each.key} must use a gateway in exactly one interface network or the peer inner address of one tunnel."
    }
  }
}

resource "pveguest_file" "interface_network" {
  for_each = local.network_interface_files

  node = local.network_guests[each.value.guest].node
  vmid = local.network_guests[each.value.guest].vmid
  kind = local.routing_guest_kind

  path = "${local.network_unit_directory}/50-rsim-${each.value.interface.name}.network"
  content = templatefile("${path.module}/files/interface.network.tftpl", {
    name = each.value.interface.name
    addresses = concat(
      each.value.interface.ipv4 != "" ? ["${each.value.interface.ipv4}/${split("/", local.network_interface_prefixes[each.value.guest][each.value.interface.name].ipv4)[1]}"] : [],
      each.value.interface.ipv6 != "" ? ["${each.value.interface.ipv6}/${split("/", local.network_interface_prefixes[each.value.guest][each.value.interface.name].ipv6)[1]}"] : [],
    )
    routes                    = [for route_index in each.value.route_indexes : local.network_guests[each.value.guest].routes[route_index]]
    unreachable_prefixes      = each.value.first ? var.unreachable_prefixes : []
    unreachable_rule_priority = local.network_unreachable_rule_priority
    unreachable_metric        = local.network_unreachable_metric
  })

  depends_on = [proxmox_virtual_environment_container.routing_simulator]
}

resource "pveguest_file" "tunnel_netdev" {
  for_each = local.network_tunnel_files

  node = local.network_guests[each.value.guest].node
  vmid = local.network_guests[each.value.guest].vmid
  kind = local.routing_guest_kind

  path = "${local.network_unit_directory}/40-rsim-${each.value.tunnel.interface}.netdev"
  content = templatefile("${path.module}/files/tunnel.netdev.tftpl", {
    name            = each.value.tunnel.interface
    kind            = local.network_tunnel_kind
    mtu             = each.value.tunnel.mtu
    outer_ipv4      = each.value.tunnel.outer_ipv4
    peer_outer_ipv4 = each.value.tunnel.peer_outer_ipv4
    outer_ttl       = each.value.tunnel.outer_ttl
  })

  depends_on = [proxmox_virtual_environment_container.routing_simulator]
}

resource "pveguest_file" "tunnel_network" {
  for_each = local.network_tunnel_files

  node = local.network_guests[each.value.guest].node
  vmid = local.network_guests[each.value.guest].vmid
  kind = local.routing_guest_kind

  path = "${local.network_unit_directory}/40-rsim-${each.value.tunnel.interface}.network"
  content = templatefile("${path.module}/files/tunnel.network.tftpl", {
    name    = each.value.tunnel.interface
    address = "${each.value.tunnel.inner_ipv6}/${each.value.tunnel.inner_prefix_length}"
    link_local = format(
      "fe80::%x:%x:%x:%x",
      each.value.octets[0] * 256 + each.value.octets[1],
      each.value.octets[2] * 256 + each.value.octets[3],
      each.value.octets[4] * 256 + each.value.octets[5],
      each.value.octets[6] * 256 + each.value.octets[7],
    )
    routes = [for route_index in each.value.route_indexes : local.network_guests[each.value.guest].routes[route_index]]
  })

  depends_on = [proxmox_virtual_environment_container.routing_simulator]
}

# restart_on restarts systemd-networkd.service when the drop-in or a network
# file changes. The drop-in retains routes and routing policy rules installed
# by FRR and other programs across the restart.
resource "pveguest_systemd_unit" "networkd" {
  for_each = local.network_guests

  node = each.value.node
  vmid = each.value.vmid
  kind = local.routing_guest_kind

  name    = "systemd-networkd.service"
  enabled = true
  active  = true

  restart_on = merge(
    { networkd_conf = pveguest_file.networkd_conf[each.key].write_id },
    { for key, file in pveguest_file.interface_network : file.path => file.write_id if local.network_interface_files[key].guest == each.key },
    { for key, file in pveguest_file.tunnel_netdev : file.path => file.write_id if local.network_tunnel_files[key].guest == each.key },
    { for key, file in pveguest_file.tunnel_network : file.path => file.write_id if local.network_tunnel_files[key].guest == each.key },
  )
}

resource "pveguest_file" "nftables" {
  for_each = local.network_guests

  node = each.value.node
  vmid = each.value.vmid
  kind = local.routing_guest_kind

  path = "/etc/nftables.conf"
  content = templatefile("${path.module}/files/nftables.conf.tftpl", {
    management_interface = each.value.management_name
    uplink_interfaces    = join(", ", [for interface in each.value.masquerade : jsonencode(interface.name)])
    uplink_sources       = [for interface in each.value.masquerade : local.routing_networks[interface.network].ipv4_net]
    tunnels              = each.value.tunnels
    bgp_peers            = each.value.bgp_peers
    transfer_server      = each.value.transfer_server
    transfer_port        = local.network_transfer_port
    forwarding           = each.value.forwarding
    forward_interfaces = join(", ", [
      for name in concat(each.value.interface_names, [for tunnel in each.value.tunnels : tunnel.interface]) : jsonencode(name)
    ])
  })
  # The Debian nftables package installs /etc/nftables.conf as an executable script.
  mode     = "0755"
  validate = "nft -c -f %s"

  depends_on = [pveguest_apt_packages.network]
}

resource "pveguest_systemd_unit" "nftables" {
  for_each = local.network_guests

  node = each.value.node
  vmid = each.value.vmid
  kind = local.routing_guest_kind

  name    = "nftables.service"
  enabled = true
  active  = true

  restart_on = {
    conf = pveguest_file.nftables[each.key].write_id
  }
}

resource "pveguest_file" "sysctl" {
  for_each = local.network_guests

  node = each.value.node
  vmid = each.value.vmid
  kind = local.routing_guest_kind

  path = "/etc/sysctl.d/90-routing-simulator.conf"
  content = templatefile("${path.module}/files/sysctl.conf.tftpl", {
    forwarding = each.value.forwarding
  })

  depends_on = [proxmox_virtual_environment_container.routing_simulator]
}

# systemd-sysctl.service applies the files in /etc/sysctl.d. The restart_on
# value makes the service apply the simulator sysctl file again after that file
# changes.
resource "pveguest_systemd_unit" "sysctl" {
  for_each = local.network_guests

  node = each.value.node
  vmid = each.value.vmid
  kind = local.routing_guest_kind

  name    = "systemd-sysctl.service"
  enabled = true
  active  = true

  restart_on = {
    conf = pveguest_file.sysctl[each.key].write_id
  }
}

# The iperf3 package leaves iperf3.service disabled. Each remote_client guest
# starts the service as a transfer target and accepts TCP port 5201.
resource "pveguest_systemd_unit" "iperf3" {
  for_each = { for name, guest in local.network_guests : name => guest if guest.transfer_server }

  node = each.value.node
  vmid = each.value.vmid
  kind = local.routing_guest_kind

  name    = "iperf3.service"
  enabled = true
  active  = true

  depends_on = [pveguest_apt_packages.network]
}
