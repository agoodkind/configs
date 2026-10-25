terraform {
  required_providers {
    pveguest = {
      source  = "tofu.home.arpa/agoodkind/pveguest"
      version = "0.1.1"
    }
  }
}

variable "node" {
  type = string
}

variable "vmid" {
  type = number
}

locals {
  files = {
    sonic = {
      path    = "/etc/systemd/network/10-sonic.network"
      content = file("${path.module}/10-sonic.network")
    }
    transit = {
      path    = "/etc/systemd/network/20-transit.network"
      content = file("${path.module}/20-transit.network")
    }
    forwarding = {
      path    = "/etc/sysctl.d/91-mwan-failover.conf"
      content = file("${path.module}/../../../../mwan-failover/sysctl.conf")
    }
  }
}

resource "pveguest_file" "config" {
  for_each = local.files

  node    = var.node
  vmid    = var.vmid
  kind    = "lxc"
  path    = each.value.path
  content = each.value.content
  mode    = "0644"
}

resource "pveguest_systemd_unit" "sysctl" {
  node    = var.node
  vmid    = var.vmid
  kind    = "lxc"
  name    = "systemd-sysctl.service"
  enabled = false
  active  = true
  restart_on = {
    forwarding = pveguest_file.config["forwarding"].write_id
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
    sonic   = pveguest_file.config["sonic"].write_id
    transit = pveguest_file.config["transit"].write_id
  }

  depends_on = [pveguest_systemd_unit.sysctl]
}

resource "pveguest_apt_packages" "this" {
  node = var.node
  vmid = var.vmid
  kind = "lxc"

  packages = toset(["ca-certificates", "curl", "ethtool", "iputils-ping", "nftables"])

  depends_on = [pveguest_systemd_unit.network]
}

resource "pveguest_file" "firewall" {
  node     = var.node
  vmid     = var.vmid
  kind     = "lxc"
  path     = "/etc/nftables.conf"
  content  = file("${path.module}/../../../../mwan-failover/nftables.conf")
  mode     = "0644"
  validate = "/usr/sbin/nft --check --file %s"

  depends_on = [pveguest_apt_packages.this]
}

resource "pveguest_systemd_unit" "firewall" {
  node    = var.node
  vmid    = var.vmid
  kind    = "lxc"
  name    = "nftables.service"
  enabled = true
  active  = true
  restart_on = {
    rules = pveguest_file.firewall.write_id
  }
}

output "vmid" {
  value = pveguest_systemd_unit.firewall.vmid
}
