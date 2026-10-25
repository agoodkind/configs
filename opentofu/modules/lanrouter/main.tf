terraform {
  required_providers {
    pveguest = {
      source  = "tofu.home.arpa/agoodkind/pveguest"
      version = "0.1.1"
    }
  }
}

variable "node" {
  description = "The value is the Proxmox node that runs the container."
  type        = string
}

variable "vmid" {
  description = "The value is the container identifier."
  type        = number
}

variable "hostname" {
  description = "The value is the hostname the module writes to the FRR configuration."
  type        = string
}

variable "transit_interface" {
  description = "The value is the name of the interface on the transit link to the gateway."
  type        = string
}

variable "transit_ipv4_address" {
  description = "The value is the transit IPv4 address and prefix length; the host part is the BGP router identifier."
  type        = string

  validation {
    condition     = can(cidrhost(var.transit_ipv4_address, 0))
    error_message = "The transit_ipv4_address value must be an IPv4 address with a prefix length, such as 10.230.230.1/29."
  }
}

variable "transit_ipv6_address" {
  description = "The value is the IPv6 address with prefix length on the transit interface."
  type        = string

  validation {
    condition     = can(cidrhost(var.transit_ipv6_address, 0))
    error_message = "The transit_ipv6_address value must be an IPv6 address with a prefix length, such as 3d06:bad:b01:3f0::1/64."
  }
}

variable "bgp_asn" {
  description = "The value is the autonomous system number shared by the router and its iBGP peer."
  type        = number
}

variable "peer_ipv4_address" {
  description = "The value is the gateway's transit IPv4 address for a BGP session that exchanges only IPv4 routes."
  type        = string
}

variable "peer_ipv6_address" {
  description = "The value is the gateway's transit IPv6 address for a BGP session that exchanges only IPv6 routes."
  type        = string
}

variable "lan_ipv4_prefixes" {
  description = "The value lists LAN IPv4 prefixes the router advertises when a connected interface has those prefixes."
  type        = list(string)

  validation {
    condition     = length(var.lan_ipv4_prefixes) > 0
    error_message = "The lan_ipv4_prefixes list must contain at least one prefix because an empty list does not define the outbound IPv4 neighbor filter."
  }
}

variable "lan_ipv6_prefixes" {
  description = "The value lists LAN IPv6 prefixes the router advertises when a connected interface has those prefixes."
  type        = list(string)

  validation {
    condition     = length(var.lan_ipv6_prefixes) > 0
    error_message = "The lan_ipv6_prefixes list must contain at least one prefix because an empty list does not define the outbound IPv6 neighbor filter."
  }
}

variable "acceptance_ipv4_prefixes" {
  description = "The value lists IPv4 acceptance test prefixes the router advertises when a connected interface has them."
  type        = list(string)
}

variable "acceptance_ipv6_prefixes" {
  description = "The value lists IPv6 acceptance test prefixes the router advertises when a connected interface has them."
  type        = list(string)
}

locals {
  kind    = "lxc"
  deb_dir = "/var/cache/lanrouter/debs"

  frr_owner = "frr"
  frr_mode  = "0640"

  bgp_router_id = split("/", var.transit_ipv4_address)[0]

  # Pin FRR and four dependencies debian-13-standard_13.6-1 lacks because the container has no uplink for apt-get.
  frr_debs = jsondecode(file("${path.module}/frr-packages.json"))
}

resource "pveguest_file" "interfaces" {
  node = var.node
  vmid = var.vmid
  kind = local.kind

  path = "/etc/network/interfaces"
  content = templatefile("${path.module}/files/interfaces.tftpl", {
    transit_interface    = var.transit_interface
    transit_ipv4_address = var.transit_ipv4_address
    transit_ipv6_address = var.transit_ipv6_address
  })
}

resource "pveguest_systemd_unit" "networking" {
  node = var.node
  vmid = var.vmid
  kind = local.kind

  name    = "networking.service"
  enabled = true
  active  = true

  restart_on = {
    interfaces = pveguest_file.interfaces.write_id
  }
}

resource "pveguest_download" "frr_deb" {
  for_each = local.frr_debs

  node = var.node
  vmid = var.vmid
  kind = local.kind

  fetch  = "controller"
  url    = each.value.url
  sha256 = each.value.sha256
  path   = "${local.deb_dir}/${each.key}.deb"
}

resource "pveguest_deb_packages" "frr" {
  node = var.node
  vmid = var.vmid
  kind = local.kind

  packages = { for name, deb in pveguest_download.frr_deb : name => deb.path }
}

resource "pveguest_file" "frr_daemons" {
  node = var.node
  vmid = var.vmid
  kind = local.kind

  path    = "/etc/frr/daemons"
  content = file("${path.module}/files/frr-daemons")
  mode    = local.frr_mode
  owner   = local.frr_owner
  group   = local.frr_owner

  depends_on = [pveguest_deb_packages.frr]
}

resource "pveguest_file" "frr_conf" {
  node = var.node
  vmid = var.vmid
  kind = local.kind

  path = "/etc/frr/frr.conf"
  content = templatefile("${path.module}/files/frr.conf.tftpl", {
    hostname          = var.hostname
    bgp_asn           = var.bgp_asn
    bgp_router_id     = local.bgp_router_id
    peer_ipv4_address = var.peer_ipv4_address
    peer_ipv6_address = var.peer_ipv6_address
    lan_ipv4_prefixes = var.lan_ipv4_prefixes
    lan_ipv6_prefixes = var.lan_ipv6_prefixes

    acceptance_ipv4_prefixes = var.acceptance_ipv4_prefixes
    acceptance_ipv6_prefixes = var.acceptance_ipv6_prefixes
  })
  mode  = local.frr_mode
  owner = local.frr_owner
  group = local.frr_owner

  validate = "/usr/bin/vtysh --dryrun --inputfile %s"

  depends_on = [pveguest_deb_packages.frr]
}

resource "pveguest_systemd_unit" "frr" {
  node = var.node
  vmid = var.vmid
  kind = local.kind

  name    = "frr.service"
  enabled = true
  active  = true

  restart_on = {
    daemons = pveguest_file.frr_daemons.write_id
    conf    = pveguest_file.frr_conf.write_id
  }

  depends_on = [
    pveguest_deb_packages.frr,
    pveguest_systemd_unit.networking,
  ]
}
