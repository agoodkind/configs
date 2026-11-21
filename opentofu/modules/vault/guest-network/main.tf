terraform {
  required_providers {
    pveguest = { source = "tofu.home.arpa/agoodkind/pveguest", version = ">= 0.1.7" }
  }
}

variable "guest" {
  type = object({
    vmid    = number
    kind    = string
    backend = string
    mac     = string
    ifname  = optional(string)
  })

  validation {
    condition     = var.guest.kind != "lxc" || try(trimspace(var.guest.ifname) != "", false)
    error_message = "Set guest.ifname to a nonblank host interface name when guest.kind is lxc."
  }
}

variable "phase" { type = string }
variable "dns_servers" { type = list(string) }

locals {
  packages = {
    netplan  = ["netplan.io"]
    networkd = []
    ifupdown = ["ifupdown", "procps", "isc-dhcp-client", "resolvconf"]
  }
}

resource "pveguest_apt_packages" "network" {
  node     = "vault"
  vmid     = var.guest.vmid
  kind     = var.guest.kind
  packages = concat(["perl", "iproute2", "systemd"], local.packages[var.guest.backend])
}

resource "pveguest_network_interface" "eth0" {
  node        = "vault"
  vmid        = var.guest.vmid
  kind        = var.guest.kind
  backend     = var.guest.backend
  name        = "eth0"
  mac         = var.guest.mac
  phase       = var.phase
  dhcp4       = true
  slaac       = true
  dns_servers = var.dns_servers
  depends_on  = [pveguest_apt_packages.network]
}

resource "pveguest_container_options" "hostnic" {
  count   = var.guest.kind == "lxc" ? 1 : 0
  node    = "vault"
  vmid    = var.guest.vmid
  options = { hostnic0 = "link=${var.guest.ifname},name=eth0" }
}
