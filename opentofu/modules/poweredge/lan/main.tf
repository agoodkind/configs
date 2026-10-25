terraform {
  required_providers {
    pveguest = {
      source  = "tofu.home.arpa/agoodkind/pveguest"
      version = "0.1.1"
    }
  }
}

resource "pveguest_download" "frr" {
  for_each = local.frr_debs

  node   = var.node
  vmid   = var.vmid
  kind   = "lxc"
  fetch  = "controller"
  url    = each.value.url
  sha256 = each.value.sha256
  path   = "/var/cache/lanrouter/debs/${each.key}.deb"
}

resource "pveguest_deb_packages" "frr" {
  node     = var.node
  vmid     = var.vmid
  kind     = "lxc"
  packages = { for name, deb in pveguest_download.frr : name => deb.path }
}

resource "pveguest_file" "network" {
  for_each = local.network_files

  node    = var.node
  vmid    = var.vmid
  kind    = "lxc"
  path    = "/etc/systemd/network/${each.key}"
  content = templatefile("${path.module}/files/${each.value}", local.template_vars)
}

resource "pveguest_file" "forwarding" {
  node    = var.node
  vmid    = var.vmid
  kind    = "lxc"
  path    = "/etc/sysctl.d/90-edge-lan.conf"
  content = file("${path.module}/files/90-edge-lan.conf")
}

resource "pveguest_systemd_unit" "sysctl" {
  node    = var.node
  vmid    = var.vmid
  kind    = "lxc"
  name    = "systemd-sysctl.service"
  enabled = false
  active  = true
  restart_on = {
    forwarding = pveguest_file.forwarding.write_id
  }
}

resource "pveguest_systemd_unit" "network" {
  node    = var.node
  vmid    = var.vmid
  kind    = "lxc"
  name    = "systemd-networkd.service"
  enabled = true
  active  = true
  restart_on = {
    for name, config in pveguest_file.network : name => config.write_id
  }

  depends_on = [pveguest_systemd_unit.sysctl]
}

resource "pveguest_file" "frr_daemons" {
  node    = var.node
  vmid    = var.vmid
  kind    = "lxc"
  path    = "/etc/frr/daemons"
  content = file("${path.module}/../../lanrouter/files/frr-daemons")
  owner   = "frr"
  group   = "frr"
  mode    = "0640"

  depends_on = [pveguest_deb_packages.frr]
}

resource "pveguest_file" "frr" {
  node     = var.node
  vmid     = var.vmid
  kind     = "lxc"
  path     = local.service_files.frr.path
  content  = file("${path.module}/files/${local.service_files.frr.source}")
  owner    = local.service_files.frr.owner
  group    = local.service_files.frr.group
  mode     = local.service_files.frr.mode
  validate = local.service_files.frr.validate

  depends_on = [pveguest_deb_packages.frr]
}

resource "pveguest_systemd_unit" "frr" {
  node    = var.node
  vmid    = var.vmid
  kind    = "lxc"
  name    = "frr.service"
  enabled = true
  active  = true
  restart_on = {
    daemons = pveguest_file.frr_daemons.write_id
    config  = pveguest_file.frr.write_id
  }

  depends_on = [pveguest_systemd_unit.network]
}

resource "pveguest_apt_packages" "lan" {
  node = var.node
  vmid = var.vmid
  kind = "lxc"
  packages = toset([
    "bind9", "bind9-dnsutils", "ca-certificates", "curl", "ethtool",
    "kea-dhcp4-server", "kea-dhcp6-server", "nftables", "nginx", "openssl", "tayga",
  ])

  depends_on = [pveguest_systemd_unit.frr]
}

resource "pveguest_file" "service" {
  for_each = { for name, config in local.service_files : name => config if name != "frr" }

  node     = var.node
  vmid     = var.vmid
  kind     = "lxc"
  path     = each.value.path
  content  = templatefile("${path.module}/files/${each.value.source}", local.template_vars)
  owner    = each.value.owner
  group    = each.value.group
  mode     = each.value.mode
  validate = each.value.validate

  depends_on = [pveguest_apt_packages.lan]
}

# VPP installation requires VPP_INSTALL_SKIP_SYSCTL, which the package resources
# cannot set. Install VPP 26.06 before applying this module.
# Preserve the guest's existing /etc/cloudflare-beacon/key.pem private key.
resource "pveguest_systemd_unit" "service" {
  for_each = local.services

  node    = var.node
  vmid    = var.vmid
  kind    = "lxc"
  name    = each.key
  enabled = true
  active  = true
  restart_on = {
    for name in each.value : name => pveguest_file.service[name].write_id
  }

  depends_on = [pveguest_systemd_unit.inactive]
}

resource "pveguest_systemd_unit" "inactive" {
  for_each = toset(["tayga.service"])

  node    = var.node
  vmid    = var.vmid
  kind    = "lxc"
  name    = each.value
  enabled = false
  active  = false

  depends_on = [pveguest_apt_packages.lan]
}
