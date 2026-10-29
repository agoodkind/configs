terraform {
  required_providers {
    pveguest = {
      source  = "tofu.home.arpa/agoodkind/pveguest"
      version = "0.1.1"
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

variable "repository_root" {
  description = "The repository_root variable specifies the absolute path of the repository checkout. The ispsim module reads ansible/inventory/group_vars/suburban_servers.yml below this path."
  type        = string
}

variable "name" {
  description = "The name variable selects the testbed_isp_lxcs entry with the same name field, such as att. The plan fails when the name matches zero entries or more than one entry."
  type        = string
}

locals {
  kind = "lxc"

  suburban_servers = yamldecode(
    file("${var.repository_root}/ansible/inventory/group_vars/suburban_servers.yml")
  )

  matching_entries = [
    for entry in local.suburban_servers.testbed_isp_lxcs : entry
    if entry.name == var.name
  ]

  # The locals below need a complete entry. This placeholder stands in when the name does not
  # select exactly one entry, and the entry_check resource then reports the error message.
  placeholder_entry = {
    ipv6_enabled = false
    dynamic_v4   = false
    v4_subnet    = ""
  }

  # The merge gives each testbed_isp_lxcs entry precedence over testbed_isp_protocol_defaults,
  # as the Ansible combine filter does.
  entry = merge(
    local.suburban_servers.testbed_isp_protocol_defaults,
    length(local.matching_entries) == 1 ? local.matching_entries[0] : local.placeholder_entry,
  )

  # The simulator object collects the entry fields that the module reads. A simulator does
  # not use a feature when its prefix or address is absent from the entry.
  simulator = {
    ipv6_enabled         = local.entry.ipv6_enabled
    dynamic_v4           = local.entry.dynamic_v4
    routed_ipv6_enabled  = try(local.entry.routed_ipv6_enabled, false)
    routed_ipv6_prefix   = try(local.entry.routed_ipv6_prefix, "")
    routed_ipv4_prefix   = try(local.entry.routed_ipv4_prefix, "")
    routed_ipv4_route_to = try(local.entry.routed_ipv4_route_to, "")
    static_v4_block      = try(local.entry.static_v4_block, "")
    static_v4_route_to   = try(local.entry.static_v4_route_to, "")
    v4_subnet            = local.entry.v4_subnet
    pd_prefix            = try(local.entry.pd_prefix, "")
    slaac_prefix         = try(local.entry.slaac_prefix, "")
    mwan_vm_ll           = try(local.entry.mwan_vm_ll, "")

    ra_min_interval_seconds              = local.entry.ra_min_interval_seconds
    ra_max_interval_seconds              = local.entry.ra_max_interval_seconds
    ra_dns_servers                       = local.entry.ra_dns_servers
    ra_dns_lifetime_seconds              = tostring(local.entry.ra_dns_lifetime_seconds)
    ra_prefix_preferred_lifetime_seconds = tostring(local.entry.ra_prefix_preferred_lifetime_seconds)
    ra_prefix_valid_lifetime_seconds     = tostring(local.entry.ra_prefix_valid_lifetime_seconds)
  }

  # Every ISP simulator container installs nftables. The package supplies nftables.service,
  # which loads the firewall and masquerade rules.
  common_packages = toset(["nftables"])

  # Only simulators with ipv6_enabled set to true install kea-dhcp6-server to delegate
  # DHCPv6 prefixes and radvd to send router advertisements.
  ipv6_packages = local.simulator.ipv6_enabled ? toset(["kea-dhcp6-server", "radvd"]) : toset([])

  # Only simulators with dynamic_v4 set to true install kea-dhcp4-server to issue DHCPv4
  # leases.
  dynamic_v4_packages = local.simulator.dynamic_v4 ? toset(["kea-dhcp4-server"]) : toset([])

  # Only simulators with ipv6_enabled set to true create the radvd configuration file and
  # radvd.service. The set contains the unit name as the for_each key for these simulators
  # and remains empty for other simulators.
  radvd_units = local.simulator.ipv6_enabled ? toset(["radvd.service"]) : toset([])

  # Only simulators that route a delegated IPv6 prefix, a routed IPv6 prefix, a routed IPv4
  # prefix, or a static IPv4 block back to the MWAN VM create pd-route.service and its
  # file. Other simulators use an empty set.
  pd_route_units = (
    local.simulator.ipv6_enabled ||
    local.simulator.routed_ipv6_enabled ||
    local.simulator.routed_ipv4_prefix != "" ||
    local.simulator.static_v4_block != ""
    ? toset(["pd-route.service"])
    : toset([])
  )
}

resource "terraform_data" "entry_check" {
  input = var.name

  lifecycle {
    precondition {
      condition     = length(local.matching_entries) == 1
      error_message = "The testbed_isp_lxcs list in ansible/inventory/group_vars/suburban_servers.yml must contain exactly one entry with name \"${var.name}\", but it contains ${length(local.matching_entries)}."
    }
  }
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
    v4_subnet           = local.simulator.v4_subnet
    static_v4_block     = local.simulator.static_v4_block
    routed_ipv4_prefix  = local.simulator.routed_ipv4_prefix
    ipv6_enabled        = local.simulator.ipv6_enabled
    routed_ipv6_enabled = local.simulator.routed_ipv6_enabled
    pd_prefix           = local.simulator.pd_prefix
    slaac_prefix        = local.simulator.slaac_prefix
    routed_ipv6_prefix  = local.simulator.routed_ipv6_prefix
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
    ra_min_interval_seconds              = local.simulator.ra_min_interval_seconds
    ra_max_interval_seconds              = local.simulator.ra_max_interval_seconds
    ra_dns_servers                       = local.simulator.ra_dns_servers
    ra_dns_lifetime_seconds              = local.simulator.ra_dns_lifetime_seconds
    slaac_prefix                         = local.simulator.slaac_prefix
    ra_prefix_valid_lifetime_seconds     = local.simulator.ra_prefix_valid_lifetime_seconds
    ra_prefix_preferred_lifetime_seconds = local.simulator.ra_prefix_preferred_lifetime_seconds
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
    ipv6_enabled         = local.simulator.ipv6_enabled
    pd_prefix            = local.simulator.pd_prefix
    mwan_vm_ll           = local.simulator.mwan_vm_ll
    routed_ipv6_enabled  = local.simulator.routed_ipv6_enabled
    routed_ipv6_prefix   = local.simulator.routed_ipv6_prefix
    routed_ipv4_prefix   = local.simulator.routed_ipv4_prefix
    routed_ipv4_route_to = local.simulator.routed_ipv4_route_to
    static_v4_block      = local.simulator.static_v4_block
    static_v4_route_to   = local.simulator.static_v4_route_to
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
