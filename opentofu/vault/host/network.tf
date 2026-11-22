resource "linux_sysctl" "guest_pf_ipv6" {
  name  = "net.ipv6.conf.${module.vault_network.guest_pf}.disable_ipv6"
  value = "1"
  file  = "/etc/sysctl.d/99-vault-guest-pf.conf"
}

resource "linux_sysctl" "forwarding" {
  for_each = toset(["net.ipv4.ip_forward", "net.ipv6.conf.all.forwarding"])
  name     = each.key
  value    = "0"
  file     = "/etc/sysctl.d/99-vault-${replace(each.key, ".", "-")}.conf"
}

resource "pveguest_host_network_interface" "guest_pf" {
  node       = "vault"
  name       = module.vault_network.guest_pf
  autostart  = true
  depends_on = [linux_sysctl.guest_pf_ipv6]
}

resource "proxmox_network_applier" "guest_pf" {
  node_name  = "vault"
  on_destroy = false
  triggers   = { interface = pveguest_host_network_interface.guest_pf.change_id }
}

resource "pveguest_host_sriov" "guests" {
  node   = "vault"
  ifname = module.vault_network.guest_pf
  numvfs = 16
  vfs = { for guest in module.vault_network.guest_networks : tostring(guest.index) => {
    mac    = guest.mac
    driver = guest.driver
    ifname = guest.ifname
  } }
  depends_on = [proxmox_network_applier.guest_pf, module.overlay]
}

resource "pveguest_host_network_interface" "management" {
  node      = "vault"
  name      = "enp4s0"
  autostart = true
  cidr      = var.management_ipv4_cidr
  cidr6     = var.management_ipv6_cidr
  gateway   = var.management_ipv4_gateway
  gateway6  = var.management_ipv6_gateway
}

resource "proxmox_network_applier" "management" {
  node_name  = "vault"
  on_destroy = false
  triggers   = { interface = pveguest_host_network_interface.management.change_id }
}
