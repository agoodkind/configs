resource "proxmox_sdn_applier" "vault_finalizer" {
  on_create = false
}

resource "proxmox_sdn_zone_simple" "vault" {
  id    = "vault"
  nodes = ["vault"]
  mtu   = 1500
  ipam  = "pve"

  depends_on = [proxmox_sdn_applier.vault_finalizer]
}

resource "proxmox_sdn_vnet" "vault" {
  id   = "vmnet"
  zone = proxmox_sdn_zone_simple.vault.id
}

resource "proxmox_sdn_subnet" "vault_ipv4" {
  cidr    = "10.250.0.0/24"
  gateway = "10.250.0.254"
  snat    = true
  vnet    = proxmox_sdn_vnet.vault.id
}

resource "proxmox_sdn_subnet" "vault_ipv6" {
  cidr    = "3d06:bad:b01::/64"
  gateway = "3d06:bad:b01::254"
  snat    = true
  vnet    = proxmox_sdn_vnet.vault.id
}

resource "proxmox_sdn_applier" "vault" {
  on_destroy = false

  depends_on = [
    proxmox_sdn_subnet.vault_ipv4,
    proxmox_sdn_subnet.vault_ipv6,
    terraform_data.guest_network_activate,
  ]

  lifecycle {
    replace_triggered_by = [
      proxmox_sdn_zone_simple.vault,
      proxmox_sdn_vnet.vault,
      proxmox_sdn_subnet.vault_ipv4,
      proxmox_sdn_subnet.vault_ipv6,
      terraform_data.guest_network_activate,
    ]
  }
}
