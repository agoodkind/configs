module "suburban_config" {
  source = "../../modules/configuration/hosts/suburban"
}

resource "proxmox_sdn_applier" "suburban_finalizer" {
  provider  = proxmox.suburban
  on_create = false
}

resource "proxmox_sdn_zone_simple" "suburban" {
  provider = proxmox.suburban
  id       = "suburban"
  nodes    = ["hypervisor"]
  mtu      = 1500
  ipam     = "pve"

  depends_on = [proxmox_sdn_applier.suburban_finalizer, module.overlay]
}

resource "proxmox_sdn_vnet" "suburban" {
  provider = proxmox.suburban
  id       = module.suburban_config.vmnet.bridge
  zone     = proxmox_sdn_zone_simple.suburban.id
}

resource "proxmox_sdn_subnet" "suburban_ipv4" {
  provider = proxmox.suburban
  cidr     = module.suburban_config.vmnet.ipv4_cidr
  gateway  = module.suburban_config.vmnet.ipv4_gateway
  snat     = true
  vnet     = proxmox_sdn_vnet.suburban.id
}

resource "proxmox_sdn_subnet" "suburban_ipv6" {
  provider = proxmox.suburban
  cidr     = module.suburban_config.vmnet.ipv6_cidr
  gateway  = module.suburban_config.vmnet.ipv6_gateway
  snat     = true
  vnet     = proxmox_sdn_vnet.suburban.id
}

resource "proxmox_sdn_applier" "suburban" {
  provider   = proxmox.suburban
  on_destroy = false

  depends_on = [
    proxmox_sdn_subnet.suburban_ipv4,
    proxmox_sdn_subnet.suburban_ipv6,
    module.suburban,
    terraform_data.uplink_ipv6_activate,
  ]

  lifecycle {
    replace_triggered_by = [
      proxmox_sdn_zone_simple.suburban,
      proxmox_sdn_vnet.suburban,
      proxmox_sdn_subnet.suburban_ipv4,
      proxmox_sdn_subnet.suburban_ipv6,
      terraform_data.uplink_ipv6_activate,
    ]
  }
}
