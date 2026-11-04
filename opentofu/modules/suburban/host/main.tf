module "config" {
  source = "../"
}

resource "proxmox_network_linux_bridge" "isp_astound_suburban" {
  node_name = "hypervisor"
  name      = module.config.bridges.isp_astound_suburban

  autostart = true

  lifecycle {
    prevent_destroy = true
  }
}

resource "proxmox_network_linux_bridge" "isp_att_suburban" {
  node_name = "hypervisor"
  name      = module.config.bridges.isp_att_suburban

  autostart = true

  lifecycle {
    prevent_destroy = true
  }
}

resource "proxmox_network_linux_bridge" "isp_mbrains_suburban" {
  node_name = "hypervisor"
  name      = module.config.bridges.isp_mbrains_suburban

  autostart = true

  lifecycle {
    prevent_destroy = true
  }
}

resource "proxmox_network_linux_bridge" "isp_routed_suburban" {
  node_name = "hypervisor"
  name      = module.config.bridges.isp_routed_suburban

  autostart = true

  lifecycle {
    prevent_destroy = true
  }
}

resource "proxmox_network_linux_bridge" "isp_webpass_suburban" {
  node_name = "hypervisor"
  name      = module.config.bridges.isp_webpass_suburban

  autostart = true

  lifecycle {
    prevent_destroy = true
  }
}

resource "proxmox_network_linux_bridge" "mwan_suburban" {
  node_name = "hypervisor"
  name      = module.config.bridges.mwan_suburban

  autostart = true

  lifecycle {
    prevent_destroy = true
  }
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

resource "proxmox_network_linux_bridge" "trunk_suburban" {
  node_name = "hypervisor"
  name      = module.config.bridges.trunk_suburban
  comment   = "MWAN-140 slice 1: VLAN-aware trunk for OPNsense iavf0 parity"

  vlan_aware = true
  vids       = "64 100 200 300"
  autostart  = true

  address  = "${local.service_mapping.vmbrtrunk_suburban.ipv4}/24"
  address6 = "${local.service_mapping.vmbrtrunk_suburban.ipv6}/64"

  lifecycle {
    prevent_destroy = true
  }
}

resource "proxmox_network_linux_bridge" "vm_management_suburban" {
  node_name = "hypervisor"
  name      = module.config.bridges.vm_management_suburban

  autostart = true
  address   = "${local.service_mapping.vmbr1_suburban.ipv4}/24"
  address6  = "fe80::1/64"

  lifecycle {
    prevent_destroy = true
  }
}

resource "proxmox_network_linux_vlan" "trunk_vlan_100_suburban" {
  node_name = "hypervisor"
  name      = "${module.config.bridges.trunk_suburban}.100"

  autostart = true
  address   = "10.240.1.5/24"

  depends_on = [
    proxmox_network_linux_bridge.trunk_suburban,
  ]

  lifecycle {
    prevent_destroy = true
  }
}

locals {
  routing_networks = var.routing_networks

  routing_management_network = "management"
  routing_created_networks = {
    for name, network in local.routing_networks : name => network
    if name != local.routing_management_network
  }
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





}

variable "routing_networks" { type = map(object({ bridge = string, ipv4_net = string, ipv6_net = string })) }

variable "service_mapping" { type = object({
  vmbrtrunk_suburban = object({
    ipv4 = string
    ipv6 = string
  })
  vmbr1_suburban = object({
    ipv4 = string
  })
}) }


locals { service_mapping = var.service_mapping }

terraform {
  required_providers {
    proxmox = {
      source  = "bpg/proxmox"
      version = ">= 0.106.0"

    }
  }
}
