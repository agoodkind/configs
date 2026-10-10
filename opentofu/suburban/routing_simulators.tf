locals {
  routing_inventory = yamldecode(
    file("${path.module}/../../ansible/inventory/group_vars/all/service_mapping.yml")
  )
  routing_networks = local.routing_inventory.testbed_routing_networks
  routing_guests = {
    for name in local.routing_inventory.testbed_routing_guests :
    name => local.service_mapping[name]
  }

  routing_management_network = "management"
  routing_created_networks = {
    for name, network in local.routing_networks : name => network
    if name != local.routing_management_network
  }
  routing_bridge_names = merge(
    { for name, bridge in proxmox_network_linux_bridge.routing_simulator : name => bridge.name },
    { (local.routing_management_network) = proxmox_network_linux_bridge.vm_management_suburban.name },
  )
  routing_existing_bridge_names = [
    proxmox_network_linux_bridge.vm_management_suburban.name,
    proxmox_network_linux_bridge.mwan_suburban.name,
    proxmox_network_linux_bridge.isp_webpass_suburban.name,
    proxmox_network_linux_bridge.isp_att_suburban.name,
    proxmox_network_linux_bridge.isp_mbrains_suburban.name,
    proxmox_network_linux_bridge.isp_astound_suburban.name,
    proxmox_network_linux_bridge.isp_routed_suburban.name,
    proxmox_network_linux_bridge.trunk_suburban.name,
  ]

  routing_management_interfaces = {
    for name, guest in local.routing_guests : name => one([
      for interface in values(guest.routing_interfaces) : interface
      if interface.network == local.routing_management_network
    ])
  }
  routing_management_ipv4_prefix_length = split("/", local.routing_networks.management.ipv4_net)[1]
  routing_management_ipv6_prefix_length = split("/", local.routing_networks.management.ipv6_net)[1]
  routing_outer_ipv4_prefix_length      = split("/", local.routing_networks.outer.ipv4_net)[1]

  routing_management = local.routing_inventory.testbed_routing_management
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

resource "proxmox_network_linux_bridge" "routing_simulator" {
  for_each  = local.routing_created_networks
  node_name = "hypervisor"
  name      = each.value.bridge

  autostart = true
  comment   = "Routing simulator network ${each.key}"

  lifecycle {
    prevent_destroy = true

    precondition {
      condition     = !contains(local.routing_existing_bridge_names, each.value.bridge)
      error_message = "Routing simulator network ${each.key} reuses the existing bridge ${each.value.bridge}."
    }

    precondition {
      condition = (
        length(distinct(values(local.routing_networks)[*].bridge)) == length(local.routing_networks)
        && local.routing_networks.management.bridge == proxmox_network_linux_bridge.vm_management_suburban.name
      )
      error_message = "testbed_routing_networks repeats a bridge, or its management entry does not use the vmbr1 bridge."
    }
  }
}

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
      error_message = "testbed_routing_guests contains two guests with the same VMID."
    }
  }
}
