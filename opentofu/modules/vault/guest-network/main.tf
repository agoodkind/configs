terraform {
  required_providers {
    pveguest = { source = "tofu.home.arpa/agoodkind/pveguest", version = ">= 0.1.10" }
  }
}

variable "guest" {
  type = object({
    vmid             = number
    kind             = string
    backend          = string
    mac              = string
    ifname           = optional(string)
    addresses        = set(string)
    secondary_mac    = string
    secondary_ifname = optional(string)
    primary_member   = string
  })

  validation {
    condition     = var.guest.kind != "lxc" || try(trimspace(var.guest.ifname) != "", false)
    error_message = "Set guest.ifname to a nonblank host interface name when guest.kind is lxc."
  }

  validation {
    condition     = var.guest.kind != "lxc" || try(trimspace(var.guest.secondary_ifname) != "" && var.guest.secondary_ifname != var.guest.ifname, false)
    error_message = "Set guest.secondary_ifname to a nonblank host interface name that differs from guest.ifname when guest.kind is lxc."
  }
}

variable "phase" { type = string }
variable "dns_servers" { type = list(string) }

variable "ipv6_proxy_neighbors" {
  type    = set(string)
  default = null
}

locals {
  packages = {
    netplan  = ["netplan.io"]
    networkd = []
    ifupdown = ["ifupdown", "procps", "isc-dhcp-client", "resolvconf"]
  }
}

resource "pveguest_apt_packages" "network" {
  node           = "vault"
  vmid           = var.guest.vmid
  kind           = var.guest.kind
  packages       = concat(["perl", "iproute2", "systemd"], local.packages[var.guest.backend])
  start_services = false
}

resource "pveguest_network_interface" "eth0" {
  node                 = "vault"
  vmid                 = var.guest.vmid
  kind                 = var.guest.kind
  backend              = var.guest.backend
  name                 = "eth0"
  mac                  = var.guest.mac
  phase                = var.phase
  dhcp4                = true
  slaac                = true
  addresses            = var.guest.addresses
  dns_servers          = var.dns_servers
  ipv6_proxy_neighbors = var.ipv6_proxy_neighbors
  depends_on           = [pveguest_apt_packages.network]

  bond = {
    members = [
      { name = "vf0", mac = var.guest.mac },
      { name = "vf1", mac = var.guest.secondary_mac },
    ]
    primary          = var.guest.primary_member
    miimon           = 100
    fail_over_mac    = "active"
    primary_reselect = "failure"
  }
}

resource "pveguest_container_options" "hostnic" {
  count = var.guest.kind == "lxc" ? 1 : 0
  node  = "vault"
  vmid  = var.guest.vmid
  options = {
    hostnic0 = "link=${var.guest.ifname},name=vf0"
    hostnic1 = "link=${var.guest.secondary_ifname},name=vf1"
  }
}
