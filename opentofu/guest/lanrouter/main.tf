terraform {
  required_providers {
    pveguest = {
      source  = "tofu.home.arpa/agoodkind/pveguest"
      version = "0.1.0"
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

locals {
  kind    = "lxc"
  deb_dir = "/var/cache/lanrouter/debs"

  frr_owner = "frr"
  frr_mode  = "0640"

  bgp_router_id = split("/", var.transit_ipv4_address)[0]

  # Pin FRR and four dependencies debian-13-standard_13.6-1 lacks because the container has no uplink for apt-get.
  frr_debs = {
    frr = {
      url    = "https://deb.debian.org/debian/pool/main/f/frr/frr_10.3-3%2bdeb13u1_amd64.deb"
      sha256 = "20718ad95f91d9bcda87769421bb33acc7a94d9f0bf0d994d31f0c6d9122ab0a"
    }
    libcares2 = {
      url    = "https://deb.debian.org/debian/pool/main/c/c-ares/libcares2_1.34.5-1%2bdeb13u1_amd64.deb"
      sha256 = "5034a2c34ea4730797df1a5f6a0ec142d7d09ce6923c06e167f08ad2e69a4218"
    }
    "liblua5.3-0" = {
      url    = "https://deb.debian.org/debian/pool/main/l/lua5.3/liblua5.3-0_5.3.6-2%2bb4_amd64.deb"
      sha256 = "35af63e29e035d7f1ce3586922c51868df86c7fd9a4d28777d80384ad809df61"
    }
    libunwind8 = {
      url    = "https://deb.debian.org/debian/pool/main/libu/libunwind/libunwind8_1.8.1-0.1_amd64.deb"
      sha256 = "db21a86dd05c93413f0ef36282a9c64d32410d240f332273a53be1408bec1f62"
    }
    libyang3 = {
      url    = "https://deb.debian.org/debian/pool/main/liby/libyang/libyang3_3.12.2-1_amd64.deb"
      sha256 = "f99414db72901df4048558e2df318deb61d9b8ac0893919dc7834f8dfd7b0743"
    }
  }
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
