terraform {
  required_providers {
    pveguest = {
      source = "tofu.home.arpa/agoodkind/pveguest"
      # pveguest 0.1.3 adds masked and allow_missing to pveguest_systemd_unit.
      version = ">= 0.1.3"
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



variable "configuration" {
  type = object({
    ipv6_enabled                         = bool
    dynamic_v4                           = bool
    routed_ipv6_enabled                  = bool
    routed_ipv6_prefix                   = string
    routed_ipv4_prefix                   = string
    routed_ipv4_route_to                 = string
    static_v4_block                      = string
    static_v4_route_to                   = string
    v4_subnet                            = string
    pd_prefix                            = string
    slaac_prefix                         = string
    mwan_vm_ll                           = string
    ra_min_interval_seconds              = number
    ra_max_interval_seconds              = number
    ra_dns_servers                       = list(string)
    ra_dns_lifetime_seconds              = string
    ra_prefix_preferred_lifetime_seconds = string
    ra_prefix_valid_lifetime_seconds     = string

    # kea-dhcp4-server uses this configuration when dynamic_v4 is true.
    # A null renew_seconds or rebind_seconds omits that timer from the Kea file.
    dhcp4 = optional(object({
      valid_lifetime_seconds         = number
      renew_seconds                  = optional(number)
      rebind_seconds                 = optional(number)
      socket_max_retries             = number
      socket_retry_wait_milliseconds = number
      pool                           = string
      gateway                        = string
      dns                            = string
      reservations = list(object({
        mac_address = string
        ipv4        = string
      }))
    }))

    # kea-dhcp6-server uses this configuration when ipv6_enabled is true.
    # Kea delegates pd_prefix at its prefix length. A non-null address_pool also
    # assigns IA_NA addresses from slaac_prefix.
    dhcp6 = optional(object({
      preferred_lifetime_seconds     = number
      valid_lifetime_seconds         = number
      renew_seconds                  = optional(number)
      rebind_seconds                 = optional(number)
      dns_servers                    = list(string)
      socket_max_retries             = number
      socket_retry_wait_milliseconds = number
      address_pool                   = optional(string)
    }))
  })

  validation {
    condition     = (var.configuration.dhcp4 != null) == var.configuration.dynamic_v4
    error_message = "Set configuration.dhcp4 exactly when configuration.dynamic_v4 is true."
  }

  validation {
    condition     = (var.configuration.dhcp6 != null) == var.configuration.ipv6_enabled
    error_message = "Set configuration.dhcp6 exactly when configuration.ipv6_enabled is true."
  }

  validation {
    condition = (
      var.configuration.dhcp6 == null ||
      try(var.configuration.dhcp6.address_pool, null) == null ||
      var.configuration.slaac_prefix != ""
    )
    error_message = "configuration.dhcp6.address_pool requires a configuration.slaac_prefix value."
  }
}

locals {
  kind = "lxc"

  simulator = var.configuration

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

  # Each map uses a Kea unit name as the key and its configuration as the value.
  # A map is empty when the simulator does not serve that DHCP family.
  dhcp4_units = {
    for dhcp4 in local.simulator.dhcp4[*] : "kea-dhcp4-server.service" => dhcp4
  }
  dhcp6_units = {
    for dhcp6 in local.simulator.dhcp6[*] : "kea-dhcp6-server.service" => dhcp6
  }

  # When IPv6 is disabled, the module stops and disables these units.
  # allow_missing permits their absence because the module does not install their packages.
  disabled_ipv6_units = (
    local.simulator.ipv6_enabled
    ? toset([])
    : toset(["kea-dhcp6-server.service", "radvd.service"])
  )

  pd_prefix_parts = split("/", local.simulator.pd_prefix)

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

resource "pveguest_file" "kea_dhcp4" {
  for_each = local.dhcp4_units

  node = var.node
  vmid = var.vmid
  kind = local.kind

  path = "/etc/kea/kea-dhcp4.conf"
  content = templatefile("${path.module}/files/kea-dhcp4.conf.tftpl", {
    socket_max_retries             = each.value.socket_max_retries
    socket_retry_wait_milliseconds = each.value.socket_retry_wait_milliseconds
    valid_lifetime_seconds         = each.value.valid_lifetime_seconds
    renew_seconds                  = each.value.renew_seconds
    rebind_seconds                 = each.value.rebind_seconds
    subnet                         = local.simulator.v4_subnet
    pool                           = each.value.pool
    gateway                        = each.value.gateway
    dns                            = each.value.dns
    reservation_lines = [
      for reservation in each.value.reservations : format(
        "                { \"hw-address\": \"%s\", \"ip-address\": \"%s\" }",
        lower(reservation.mac_address),
        reservation.ipv4,
      )
    ]
  })
  validate = "kea-dhcp4 -t %s"

  depends_on = [pveguest_apt_packages.simulator]
}

resource "pveguest_systemd_unit" "kea_dhcp4" {
  for_each = local.dhcp4_units

  node = var.node
  vmid = var.vmid
  kind = local.kind

  name    = each.key
  enabled = true
  active  = true

  restart_on = {
    conf = pveguest_file.kea_dhcp4[each.key].write_id
  }

  depends_on = [pveguest_apt_packages.simulator]
}

resource "pveguest_file" "kea_dhcp6" {
  for_each = local.dhcp6_units

  node = var.node
  vmid = var.vmid
  kind = local.kind

  path = "/etc/kea/kea-dhcp6.conf"
  content = templatefile("${path.module}/files/kea-dhcp6.conf.tftpl", {
    socket_max_retries             = each.value.socket_max_retries
    socket_retry_wait_milliseconds = each.value.socket_retry_wait_milliseconds
    preferred_lifetime_seconds     = each.value.preferred_lifetime_seconds
    valid_lifetime_seconds         = each.value.valid_lifetime_seconds
    renew_seconds                  = each.value.renew_seconds
    rebind_seconds                 = each.value.rebind_seconds
    dns_servers                    = each.value.dns_servers
    address_pool                   = each.value.address_pool
    slaac_prefix                   = local.simulator.slaac_prefix
    pd_network                     = local.pd_prefix_parts[0]
    pd_length                      = local.pd_prefix_parts[1]
  })
  validate = "kea-dhcp6 -t %s"

  depends_on = [pveguest_apt_packages.simulator]
}

resource "pveguest_systemd_unit" "kea_dhcp6" {
  for_each = local.dhcp6_units

  node = var.node
  vmid = var.vmid
  kind = local.kind

  name    = each.key
  enabled = true
  active  = true

  restart_on = {
    conf = pveguest_file.kea_dhcp6[each.key].write_id
  }

  depends_on = [pveguest_apt_packages.simulator]
}

resource "pveguest_file" "kea_rebind_script" {
  node = var.node
  vmid = var.vmid
  kind = local.kind

  path    = "/usr/local/sbin/kea-rebind"
  content = file("${path.module}/files/kea-rebind.sh")
  mode    = "0755"
}

resource "pveguest_file" "kea_rebind_unit" {
  node = var.node
  vmid = var.vmid
  kind = local.kind

  path    = "/etc/systemd/system/kea-rebind.service"
  content = file("${path.module}/files/kea-rebind.service")
}

resource "pveguest_systemd_unit" "kea_rebind" {
  node = var.node
  vmid = var.vmid
  kind = local.kind

  name    = "kea-rebind.service"
  enabled = true
  active  = true

  restart_on = {
    script = pveguest_file.kea_rebind_script.write_id
    unit   = pveguest_file.kea_rebind_unit.write_id
  }

  depends_on = [
    pveguest_systemd_unit.kea_dhcp4,
    pveguest_systemd_unit.kea_dhcp6,
    pveguest_systemd_unit.pd_route,
  ]
}

resource "pveguest_systemd_unit" "disabled_ipv6" {
  for_each = local.disabled_ipv6_units

  node = var.node
  vmid = var.vmid
  kind = local.kind

  name          = each.key
  enabled       = false
  active        = false
  allow_missing = true
}

# Kea serves DHCP on every simulator. Some LXC base images enable dnsmasq,
# which conflicts with Kea for the DHCP socket and fails at every boot.
# The module masks dnsmasq.service and clears its failed state if the unit exists.
resource "pveguest_systemd_unit" "dnsmasq" {
  node = var.node
  vmid = var.vmid
  kind = local.kind

  name         = "dnsmasq.service"
  enabled      = false
  active       = false
  masked       = true
  reset_failed = true
}
