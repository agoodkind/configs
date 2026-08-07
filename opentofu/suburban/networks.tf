# Suburban testbed network resources managed through the Proxmox API.
#
# OpenTofu owns the PVE-readable testbed bridge shape. Ansible still owns
# sourced runtime fragments that the Proxmox network API cannot model cleanly:
# NAT masquerade rules and the extra routable vmbr1 IPv6 address.

resource "proxmox_network_linux_bridge" "vm_management_suburban" {
  node_name = "hypervisor"
  name      = "vmbr1"

  autostart = true
  address   = "${local.service_mapping.vmbr1_suburban.ipv4}/24"
  address6  = "fe80::1/64"

  lifecycle {
    prevent_destroy = true
  }
}

resource "proxmox_network_linux_bridge" "mwan_suburban" {
  node_name = "hypervisor"
  name      = "vmbr2"

  autostart = true

  lifecycle {
    prevent_destroy = true
  }
}

resource "proxmox_network_linux_bridge" "isp_webpass_suburban" {
  node_name = "hypervisor"
  name      = "vmbr4"

  autostart = true

  lifecycle {
    prevent_destroy = true
  }
}

resource "proxmox_network_linux_bridge" "isp_att_suburban" {
  node_name = "hypervisor"
  name      = "vmbr5"

  autostart = true

  lifecycle {
    prevent_destroy = true
  }
}

resource "proxmox_network_linux_bridge" "isp_mbrains_suburban" {
  node_name = "hypervisor"
  name      = "vmbr6"

  autostart = true

  lifecycle {
    prevent_destroy = true
  }
}

resource "proxmox_network_linux_bridge" "trunk_suburban" {
  node_name = "hypervisor"
  name      = "vmbrtrunk"
  comment   = "MWAN-140 slice 1: VLAN-aware trunk for OPNsense iavf0 parity"

  vlan_aware = true
  vids       = "64 100 200 300"
  autostart  = true

  address  = "10.240.4.5/24"
  address6 = "3d06:bad:b01:210::5/64"

  lifecycle {
    prevent_destroy = true
  }
}

resource "proxmox_network_linux_vlan" "trunk_vlan_100_suburban" {
  node_name = "hypervisor"
  name      = "vmbrtrunk.100"

  autostart = true
  address   = "10.240.1.5/24"

  depends_on = [
    proxmox_network_linux_bridge.trunk_suburban,
  ]

  lifecycle {
    prevent_destroy = true
  }
}
