resource "proxmox_virtual_environment_container" "routing_simulator" {
  for_each  = local.routing_guests
  node_name = "hypervisor"
  vm_id     = each.value.vmid

  initialization {
    hostname = each.value.hostname
    dns {
      servers = ["2606:4700:4700::1111", "1.1.1.1"]
    }
    ip_config {
      ipv4 {
        address = "${local.routing_management_interfaces[each.key].ipv4}/${local.routing_management_ipv4_prefix_length}"
        gateway = local.routing_management_ipv4_gateway
      }
      ipv6 {
        address = "${local.routing_management_interfaces[each.key].ipv6}/${local.routing_management_ipv6_prefix_length}"
        gateway = local.routing_management_ipv6_gateway
      }
    }
    user_account {
      keys = [var.ssh_keys]
    }
  }

  # The configuration omits the features block because the API token cannot
  # write feature flags.

  dynamic "network_interface" {
    for_each = {
      for interface in values(each.value.routing_interfaces) : interface.name => interface
    }

    content {
      name        = network_interface.key
      bridge      = local.routing_bridge_names[network_interface.value.network]
      mac_address = network_interface.value.mac_address
    }
  }

  disk {
    datastore_id = local.routing_guest_disk_datastore
    size         = local.routing_guest_disk_gigabytes
  }

  memory {
    dedicated = local.routing_guest_memory_megabytes
    swap      = local.routing_guest_memory_megabytes
  }

  cpu {
    architecture = "amd64"
    cores        = 1
  }

  tags = ["lxc", "mwan", "testbed"]

  operating_system {
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started       = true
  start_on_boot = true
  unprivileged  = true

  lifecycle {
    prevent_destroy = false
    ignore_changes = [
      initialization[0].user_account,
      operating_system[0].template_file_id,
    ]

    precondition {
      condition     = local.routing_management_interfaces[each.key].name == local.routing_management_interface_name
      error_message = "Routing simulator guest ${each.key} must attach its management interface as ${local.routing_management_interface_name}."
    }

    precondition {
      condition     = length(distinct(values(local.routing_guests)[*].vmid)) == length(local.routing_guests)
      error_message = "Routing guests must have distinct VMIDs."
    }
  }
}

locals {
  routing_networks = var.routing_networks
  routing_guests   = { for name in var.guest_keys : name => local.service_mapping[name] }

  routing_management_network = "management"
  routing_bridge_names       = var.host_networks

  routing_management_interfaces = {
    for name, guest in local.routing_guests : name => one([
      for interface in values(guest.routing_interfaces) : interface
      if interface.network == local.routing_management_network
    ])
  }
  routing_management_ipv4_prefix_length = split("/", local.routing_networks.management.ipv4_net)[1]
  routing_management_ipv6_prefix_length = split("/", local.routing_networks.management.ipv6_net)[1]

  routing_management = var.routing_management
  routing_management_ipv4_gateway = (
    local.routing_management.ipv4_default_route ? local.service_mapping.vmbr1_suburban.ipv4 : null
  )
  routing_management_ipv6_gateway = (
    local.routing_management.ipv6_default_route ? local.service_mapping.vmbr1_suburban.ipv6 : null
  )

  # Proxmox applies the single ip_config block to the first interface.
  routing_management_interface_name = "eth0"

  routing_guest_memory_megabytes = 512
  routing_guest_disk_gigabytes   = 4

  # The simulator root disks use the slow storage tier because rpool has disk
  # IO timeouts (TACK-483).
  routing_guest_disk_datastore = "slow-zfs"
}

variable "guest_keys" { type = set(string) }

variable "routing_networks" { type = map(object({ bridge = string, ipv4_net = string, ipv6_net = string })) }

variable "service_mapping" { type = map(object({ hostname = optional(string), node = optional(string), vmid = optional(number), ipv4 = optional(string), ipv6 = optional(string), routing_interfaces = optional(map(object({ name = string, network = string, mac_address = string, ipv4 = string, ipv6 = string }))) })) }

variable "ssh_keys" { type = string }

variable "host_networks" { type = map(string) }

locals { service_mapping = var.service_mapping }

terraform {
  required_providers {
    proxmox = {
      source  = "bpg/proxmox"
      version = ">= 0.106.0"

    }
    pveguest = {
      source = "tofu.home.arpa/agoodkind/pveguest"
      # pveguest 0.1.3 adds the allow_missing attribute of pveguest_systemd_unit.
      version = ">= 0.1.3"
    }
  }
}

variable "routing_management" { type = object({ forwarding = bool, ipv4_default_route = bool, ipv6_default_route = bool }) }

# Each key is a guest key from guest_keys. FRR runs bgpd on that guest with
# this configuration. FRR stays stopped and disabled on every other guest.
variable "frr" {
  type = map(object({
    asn                                = number
    router_id                          = string
    graceful_restart_stalepath_seconds = optional(number)
    default_route_interface            = optional(string)
    networks                           = list(string)
    tracked_routes = list(object({
      prefix    = string
      interface = string
    }))
    peers = list(object({
      id                = string
      address           = string
      asn               = number
      update_source     = string
      keepalive_seconds = number
      hold_seconds      = number
      multihop_ttl      = number
      import_prefixes   = list(string)
      export_prefixes   = list(string)
    }))
  }))
  default = {}

  validation {
    condition     = alltrue([for key in keys(var.frr) : contains(var.guest_keys, key)])
    error_message = "Each frr key must be a guest key in guest_keys."
  }
}

locals {
  routing_guest_kind = "lxc"

  frr_guests     = { for name, guest in local.routing_guests : name => guest if contains(keys(var.frr), name) }
  non_frr_guests = { for name, guest in local.routing_guests : name => guest if !contains(keys(var.frr), name) }

  # The template originates this route from the service interface.
  frr_default_route = "::/0"
}

resource "pveguest_apt_packages" "frr" {
  for_each = local.frr_guests

  node = each.value.node
  vmid = each.value.vmid
  kind = local.routing_guest_kind

  packages = toset(["frr"])

  depends_on = [proxmox_virtual_environment_container.routing_simulator]
}

resource "pveguest_file" "frr_daemons" {
  for_each = local.frr_guests

  node = each.value.node
  vmid = each.value.vmid
  kind = local.routing_guest_kind

  path    = "/etc/frr/daemons"
  content = file("${path.module}/files/frr-daemons")
  mode    = "0640"
  owner   = "frr"
  group   = "frr"

  depends_on = [pveguest_apt_packages.frr]
}

resource "pveguest_file" "frr_conf" {
  for_each = local.frr_guests

  node = each.value.node
  vmid = each.value.vmid
  kind = local.routing_guest_kind

  path = "/etc/frr/frr.conf"
  content = templatefile("${path.module}/files/frr.conf.tftpl", {
    hostname                           = split(".", each.value.hostname)[0]
    asn                                = var.frr[each.key].asn
    router_id                          = var.frr[each.key].router_id
    graceful_restart_stalepath_seconds = var.frr[each.key].graceful_restart_stalepath_seconds
    default_route                      = local.frr_default_route
    default_route_interface            = var.frr[each.key].default_route_interface
    networks                           = var.frr[each.key].networks
    tracked_routes                     = var.frr[each.key].tracked_routes
    peers                              = var.frr[each.key].peers
  })
  mode     = "0640"
  owner    = "frr"
  group    = "frr"
  validate = "vtysh -C -f %s"

  depends_on = [pveguest_apt_packages.frr]
}

resource "pveguest_systemd_unit" "frr" {
  for_each = local.frr_guests

  node = each.value.node
  vmid = each.value.vmid
  kind = local.routing_guest_kind

  name    = "frr.service"
  enabled = true
  active  = true

  restart_on = {
    daemons = pveguest_file.frr_daemons[each.key].write_id
    conf    = pveguest_file.frr_conf[each.key].write_id
  }
}

resource "pveguest_systemd_unit" "frr_disabled" {
  for_each = local.non_frr_guests

  node = each.value.node
  vmid = each.value.vmid
  kind = local.routing_guest_kind

  name          = "frr.service"
  enabled       = false
  active        = false
  allow_missing = true

  depends_on = [proxmox_virtual_environment_container.routing_simulator]
}
