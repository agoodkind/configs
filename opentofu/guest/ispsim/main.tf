terraform {
  required_providers {
    pveguest = {
      source  = "tofu.home.arpa/agoodkind/pveguest"
      version = "0.1.0"
    }
  }
}

variable "node" {
  description = "The node variable specifies the hypervisor key in the pveguest provider's nodes map. The ispsim module sets node on every pveguest resource."
  type        = string
}

variable "vmid" {
  description = "The vmid variable specifies the Proxmox ID of the ISP simulator container that the module configures."
  type        = number
}

variable "simulator" {
  description = "The simulator variable contains one testbed_isp_lxcs entry that overrides testbed_isp_protocol_defaults, and the module reads its fields to choose packages, render the nftables, radvd, and pd-route files, and declare their units. The simulator does not use a feature when its prefix or address is absent."
  type = object({
    ipv6_enabled         = bool
    dynamic_v4           = bool
    routed_ipv6_enabled  = optional(bool, false)
    routed_ipv6_prefix   = optional(string, "")
    routed_ipv4_prefix   = optional(string, "")
    routed_ipv4_route_to = optional(string, "")
    static_v4_block      = optional(string, "")
    static_v4_route_to   = optional(string, "")
    v4_subnet            = string
    pd_prefix            = optional(string, "")
    slaac_prefix         = optional(string, "")
    mwan_vm_ll           = optional(string, "")

    ra_min_interval_seconds              = number
    ra_max_interval_seconds              = number
    ra_dns_servers                       = list(string)
    ra_dns_lifetime_seconds              = string
    ra_prefix_preferred_lifetime_seconds = string
    ra_prefix_valid_lifetime_seconds     = string
  })
}

locals {
  kind = "lxc"

  # Every ISP simulator container installs nftables. The package supplies nftables.service,
  # which loads the firewall and masquerade rules.
  common_packages = toset(["nftables"])

  # Only simulators with ipv6_enabled set to true install kea-dhcp6-server to delegate
  # DHCPv6 prefixes and radvd to send router advertisements.
  ipv6_packages = var.simulator.ipv6_enabled ? toset(["kea-dhcp6-server", "radvd"]) : toset([])

  # Only simulators with dynamic_v4 set to true install kea-dhcp4-server to issue DHCPv4
  # leases.
  dynamic_v4_packages = var.simulator.dynamic_v4 ? toset(["kea-dhcp4-server"]) : toset([])

  # Only simulators with ipv6_enabled set to true create the radvd configuration file and
  # radvd.service. The set contains the unit name as the for_each key for these simulators
  # and remains empty for other simulators.
  radvd_units = var.simulator.ipv6_enabled ? toset(["radvd.service"]) : toset([])

  # Only simulators that route a delegated IPv6 prefix, a routed IPv6 prefix, a routed IPv4
  # prefix, or a static IPv4 block back to the MWAN VM create pd-route.service and its
  # file. Other simulators use an empty set.
  pd_route_units = (
    var.simulator.ipv6_enabled ||
    var.simulator.routed_ipv6_enabled ||
    var.simulator.routed_ipv4_prefix != "" ||
    var.simulator.static_v4_block != ""
    ? toset(["pd-route.service"])
    : toset([])
  )
}

resource "pveguest_apt_packages" "simulator" {
  node = var.node
  vmid = var.vmid
  kind = local.kind

  packages = setunion(
    local.common_packages,
    local.ipv6_packages,
    local.dynamic_v4_packages,
  )
}

resource "pveguest_file" "nftables" {
  node = var.node
  vmid = var.vmid
  kind = local.kind

  path = "/etc/nftables.conf"
  content = templatefile("${path.module}/files/nftables.conf.tftpl", {
    v4_subnet           = var.simulator.v4_subnet
    static_v4_block     = var.simulator.static_v4_block
    routed_ipv4_prefix  = var.simulator.routed_ipv4_prefix
    ipv6_enabled        = var.simulator.ipv6_enabled
    routed_ipv6_enabled = var.simulator.routed_ipv6_enabled
    pd_prefix           = var.simulator.pd_prefix
    slaac_prefix        = var.simulator.slaac_prefix
    routed_ipv6_prefix  = var.simulator.routed_ipv6_prefix
  })

  depends_on = [pveguest_apt_packages.simulator]
}

resource "pveguest_systemd_unit" "nftables" {
  node = var.node
  vmid = var.vmid
  kind = local.kind

  name    = "nftables.service"
  enabled = true
  active  = true

  restart_on = {
    conf = pveguest_file.nftables.write_id
  }

  depends_on = [pveguest_apt_packages.simulator]
}

resource "pveguest_file" "sysctl" {
  node = var.node
  vmid = var.vmid
  kind = local.kind

  path    = "/etc/sysctl.d/99-isp.conf"
  content = file("${path.module}/files/sysctl-isp.conf")
}

# systemd-sysctl.service applies the files in /etc/sysctl.d. The restart_on value makes the
# service apply the simulator sysctl file again after that file changes.
resource "pveguest_systemd_unit" "sysctl" {
  node = var.node
  vmid = var.vmid
  kind = local.kind

  name    = "systemd-sysctl.service"
  enabled = true
  active  = true

  restart_on = {
    conf = pveguest_file.sysctl.write_id
  }
}

resource "pveguest_file" "radvd" {
  for_each = local.radvd_units

  node = var.node
  vmid = var.vmid
  kind = local.kind

  path = "/etc/radvd.conf"
  content = templatefile("${path.module}/files/radvd.conf.tftpl", {
    ra_min_interval_seconds              = var.simulator.ra_min_interval_seconds
    ra_max_interval_seconds              = var.simulator.ra_max_interval_seconds
    ra_dns_servers                       = var.simulator.ra_dns_servers
    ra_dns_lifetime_seconds              = var.simulator.ra_dns_lifetime_seconds
    slaac_prefix                         = var.simulator.slaac_prefix
    ra_prefix_valid_lifetime_seconds     = var.simulator.ra_prefix_valid_lifetime_seconds
    ra_prefix_preferred_lifetime_seconds = var.simulator.ra_prefix_preferred_lifetime_seconds
  })

  depends_on = [pveguest_apt_packages.simulator]
}

resource "pveguest_systemd_unit" "radvd" {
  for_each = local.radvd_units

  node = var.node
  vmid = var.vmid
  kind = local.kind

  name    = each.key
  enabled = true
  active  = true

  restart_on = {
    conf = pveguest_file.radvd[each.key].write_id
  }

  depends_on = [pveguest_apt_packages.simulator]
}

resource "pveguest_file" "pd_route" {
  for_each = local.pd_route_units

  node = var.node
  vmid = var.vmid
  kind = local.kind

  path = "/etc/systemd/system/pd-route.service"
  content = templatefile("${path.module}/files/pd-route.service.tftpl", {
    ipv6_enabled         = var.simulator.ipv6_enabled
    pd_prefix            = var.simulator.pd_prefix
    mwan_vm_ll           = var.simulator.mwan_vm_ll
    routed_ipv6_enabled  = var.simulator.routed_ipv6_enabled
    routed_ipv6_prefix   = var.simulator.routed_ipv6_prefix
    routed_ipv4_prefix   = var.simulator.routed_ipv4_prefix
    routed_ipv4_route_to = var.simulator.routed_ipv4_route_to
    static_v4_block      = var.simulator.static_v4_block
    static_v4_route_to   = var.simulator.static_v4_route_to
  })
}

resource "pveguest_systemd_unit" "pd_route" {
  for_each = local.pd_route_units

  node = var.node
  vmid = var.vmid
  kind = local.kind

  name    = each.key
  enabled = true
  active  = true

  restart_on = {
    unit = pveguest_file.pd_route[each.key].write_id
  }
}
