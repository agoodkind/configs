resource "proxmox_virtual_environment_container" "isp_astound_suburban" {
  provider  = proxmox.root
  node_name = "hypervisor"
  vm_id     = local.service_mapping.isp_astound_suburban.vmid


  initialization {
    hostname = local.service_mapping.isp_astound_suburban.hostname
    user_account {
      keys = [var.ssh_keys]
    }
    dns {
      servers = ["1.1.1.1"]
    }
    # The real Astound circuit carries no IPv6, so this sim is IPv4-only on
    # both links: no IPv6 uplink address, no IPv6 gateway, and (unlike the
    # other three sims) no IA_NA, PD, or SLAAC on the segment side either.
    ip_config {
      ipv4 {
        address = "${local.service_mapping.isp_astound_suburban.ipv4}/24"
      }
    }
    ip_config {
      ipv4 {
        address = "${local.service_mapping.isp_astound_suburban.ipv4_uplink}/24"
        gateway = local.service_mapping.vmbr1_suburban.ipv4
      }
    }
    ip_config {
      ipv4 {
        address = "${local.service_mapping.isp_astound_suburban.routing_interfaces.outer.ipv4}/${local.routing_outer_ipv4_prefix_length}"
      }
    }
  }

  features {
    nesting = true
  }

  network_interface {
    name        = "eth0"
    bridge      = var.host_networks.isp_astound_suburban
    mac_address = "BC:24:11:A5:70:04"
  }

  network_interface {
    name        = "eth1"
    bridge      = var.host_networks.vm_management_suburban
    mac_address = "BC:24:11:A5:70:05"
  }

  # The provider restarts container 903 during the apply that adds this
  # interface.
  network_interface {
    name        = local.service_mapping.isp_astound_suburban.routing_interfaces.outer.name
    bridge      = local.routing_bridge_names[local.service_mapping.isp_astound_suburban.routing_interfaces.outer.network]
    mac_address = local.service_mapping.isp_astound_suburban.routing_interfaces.outer.mac_address
  }

  disk {
    datastore_id = "local-zfs"
    size         = 2
  }

  memory {
    dedicated = 512
    swap      = 512
  }

  cpu {
    architecture = "amd64"
    cores        = 1
    limit        = 0
  }

  console {
    enabled   = true
    tty_count = 2
    type      = "tty"
  }

  tags = []

  operating_system {
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started       = true
  start_on_boot = true
  unprivileged  = false

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      initialization[0].user_account,
      operating_system[0].template_file_id,
    ]
  }
}

locals {
  routing_outer_ipv4_prefix_length = split("/", var.routing_inventory.testbed_routing_networks.outer.ipv4_net)[1]
  routing_bridge_names             = var.host_networks
}

variable "routing_inventory" { type = any }

variable "service_mapping" { type = any }

variable "ssh_keys" { type = string }

variable "host_networks" { type = map(string) }

locals { service_mapping = var.service_mapping }

terraform {
  required_providers {
    proxmox = {
      source                = "bpg/proxmox"
      version               = ">= 0.106.0"
      configuration_aliases = [proxmox.root]
    }
  }
}
