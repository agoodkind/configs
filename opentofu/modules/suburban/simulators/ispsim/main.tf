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
  description = "The name variable selects the testbed_isp_lxcs entry with the same name field, such as att. The plan fails with an invalid index error when no entry has the name, and with a duplicate object key error when more than one entry has the name."
  type        = string
}

variable "service_mapping" {
  description = "The module uses this decoded service mapping to resolve v4_reservations placeholders. A missing guest or field causes an invalid index error during planning."
  type        = any
}

variable "management_public_keys" {
  description = "The module sorts these SSH public keys, removes duplicates, and writes them to the mwan-testbed key file."
  type        = list(string)

  validation {
    condition     = length(var.management_public_keys) > 0
    error_message = "management_public_keys must contain at least one SSH public key because an empty key file locks the management user out of the simulator."
  }

  validation {
    condition = alltrue([
      for key in var.management_public_keys : can(regex(
        "^(ssh-[A-Za-z0-9@._+-]+|ecdsa-[A-Za-z0-9@._+-]+|sk-[A-Za-z0-9@._+-]+) [A-Za-z0-9+/]+={0,3}( .*)?$",
        trimspace(key),
      ))
    ])
    error_message = "Each `management_public_keys` entry must match the SSH public key pattern: a key type starting with `ssh-`, `ecdsa-`, or `sk-`, followed by base64 data and an optional comment."
  }
}

locals {
  kind = "lxc"

  suburban_servers = yamldecode(
    file("${var.repository_root}/ansible/inventory/group_vars/suburban_servers.yml")
  )

  # Building a map keyed by name fails the plan with "Duplicate object key" when two entries
  # share a name, and the lookup below fails with "Invalid index" when no entry has the name.
  entries_by_name = {
    for entry in local.suburban_servers.testbed_isp_lxcs : entry.name => entry
  }

  # The merge gives each testbed_isp_lxcs entry precedence over testbed_isp_protocol_defaults,
  # as the Ansible combine filter does.
  entry = merge(
    local.suburban_servers.testbed_isp_protocol_defaults,
    local.entries_by_name[var.name],
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

    ia_na                            = try(local.entry.ia_na, false)
    ia_na_pool                       = try(local.entry.ia_na_pool, "")
    pd_len                           = try(local.entry.pd_len, 0)
    dhcp6_preferred_lifetime_seconds = local.entry.dhcp6_preferred_lifetime_seconds
    dhcp6_valid_lifetime_seconds     = local.entry.dhcp6_valid_lifetime_seconds
    dhcp6_renew_seconds              = tostring(local.entry.dhcp6_renew_seconds)
    dhcp6_rebind_seconds             = tostring(local.entry.dhcp6_rebind_seconds)
    dhcp6_dns_servers                = local.entry.dhcp6_dns_servers
    dhcp6_socket_max_retries         = local.entry.dhcp6_socket_max_retries
    dhcp6_socket_retry_wait_ms       = local.entry.dhcp6_socket_retry_wait_ms
    dhcp4_valid_lifetime_seconds     = local.entry.dhcp4_valid_lifetime_seconds
    dhcp4_renew_seconds              = tostring(local.entry.dhcp4_renew_seconds)
    dhcp4_rebind_seconds             = tostring(local.entry.dhcp4_rebind_seconds)
    dhcp4_socket_max_retries         = local.entry.dhcp4_socket_max_retries
    dhcp4_socket_retry_wait_ms       = local.entry.dhcp4_socket_retry_wait_ms
    v4_gateway                       = try(local.entry.v4_gateway, "")
    v4_pool                          = try(local.entry.v4_pool, "")
    v4_dns                           = try(local.entry.v4_dns, "")
    v4_reservations                  = local.v4_reservations
  }

  # Each v4_reservations address is a literal IPv4 address or a {{service_mapping.<guest>.<field>}} placeholder.
  # The regex captures the guest key first and the field second.
  # The module resolves placeholders against service_mapping and leaves literal addresses unchanged.
  reservation_placeholder_pattern = "^\\{\\{ service_mapping\\.([a-z0-9_]+)\\.([a-z0-9_]+) \\}\\}$"

  v4_reservations = [
    for reservation in try(local.entry.v4_reservations, []) : {
      mac = reservation.mac
      addr = (
        can(regex(local.reservation_placeholder_pattern, reservation.addr))
        ? var.service_mapping[regex(local.reservation_placeholder_pattern, reservation.addr)[0]][regex(local.reservation_placeholder_pattern, reservation.addr)[1]]
        : reservation.addr
      )
    }
  ]

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

  # Only simulators with ipv6_enabled enabled create DHCPv6 units.
  # Only simulators with dynamic_v4 enabled create DHCPv4 units.
  # Each corresponding set contains the unit name when the scenario enables the family and stays empty otherwise.
  kea_dhcp6_units = local.simulator.ipv6_enabled ? toset(["kea-dhcp6-server.service"]) : toset([])
  kea_dhcp4_units = local.simulator.dynamic_v4 ? toset(["kea-dhcp4-server.service"]) : toset([])

  isp_lxc_dir = "${var.repository_root}/testbed/isp-lxc"

  management_keys_path = "/etc/ssh/authorized_keys.d/mwan-testbed"
  management_keys      = sort(distinct([for key in var.management_public_keys : trimspace(key)]))
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

# Ansible retains cleanup operations that the module does not declare.
# Ansible stops and disables Kea families that the scenario does not enable when their units exist.
# Ansible stops, disables, and masks leftover dnsmasq, resets failed units, and deletes the obsolete sshd drop-in 00-mwan-testbed-management.conf.
# pveguest_systemd_unit requires existing units and cannot mask units or reset failed units.
# pveguest_file cannot remove files.
# Declare these operations after a pinned pveguest release adds unit masking, tolerance for absent units, and file removal.

resource "pveguest_file" "kea_dhcp6" {
  for_each = local.kea_dhcp6_units

  node = var.node
  vmid = var.vmid
  kind = local.kind

  path = "/etc/kea/kea-dhcp6.conf"
  content = templatefile("${path.module}/files/kea-dhcp6.conf.tftpl", {
    socket_max_retries         = local.simulator.dhcp6_socket_max_retries
    socket_retry_wait_ms       = local.simulator.dhcp6_socket_retry_wait_ms
    preferred_lifetime_seconds = local.simulator.dhcp6_preferred_lifetime_seconds
    valid_lifetime_seconds     = local.simulator.dhcp6_valid_lifetime_seconds
    renew_seconds              = local.simulator.dhcp6_renew_seconds
    rebind_seconds             = local.simulator.dhcp6_rebind_seconds
    dns_servers                = local.simulator.dhcp6_dns_servers
    ia_na                      = local.simulator.ia_na
    slaac_prefix               = local.simulator.slaac_prefix
    ia_na_pool                 = local.simulator.ia_na_pool
    pd_prefix                  = local.simulator.pd_prefix
    pd_len                     = local.simulator.pd_len
  })

  depends_on = [pveguest_apt_packages.simulator]
}

resource "pveguest_systemd_unit" "kea_dhcp6" {
  for_each = local.kea_dhcp6_units

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

resource "pveguest_file" "kea_dhcp4" {
  for_each = local.kea_dhcp4_units

  node = var.node
  vmid = var.vmid
  kind = local.kind

  path = "/etc/kea/kea-dhcp4.conf"
  content = templatefile("${path.module}/files/kea-dhcp4.conf.tftpl", {
    socket_max_retries     = local.simulator.dhcp4_socket_max_retries
    socket_retry_wait_ms   = local.simulator.dhcp4_socket_retry_wait_ms
    valid_lifetime_seconds = local.simulator.dhcp4_valid_lifetime_seconds
    renew_seconds          = local.simulator.dhcp4_renew_seconds
    rebind_seconds         = local.simulator.dhcp4_rebind_seconds
    v4_subnet              = local.simulator.v4_subnet
    v4_pool                = local.simulator.v4_pool
    v4_gateway             = local.simulator.v4_gateway
    v4_dns                 = local.simulator.v4_dns
    v4_reservations        = local.simulator.v4_reservations
  })

  depends_on = [pveguest_apt_packages.simulator]
}

resource "pveguest_systemd_unit" "kea_dhcp4" {
  for_each = local.kea_dhcp4_units

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

# kea-rebind restarts the Kea services and pd-route when the eth0 interface index changes.
# The unit restarts when the script or unit file changes to apply either update.
resource "pveguest_file" "kea_rebind_script" {
  node = var.node
  vmid = var.vmid
  kind = local.kind

  path    = "/usr/local/sbin/kea-rebind"
  content = file("${local.isp_lxc_dir}/kea-rebind.sh")
  mode    = "0755"
}

resource "pveguest_file" "kea_rebind_unit" {
  node = var.node
  vmid = var.vmid
  kind = local.kind

  path    = "/etc/systemd/system/kea-rebind.service"
  content = file("${local.isp_lxc_dir}/kea-rebind.service")
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
}

# This resource writes the testbed operator's GitHub SSH public keys.
# The base module configures sshd to read this file but does not write it.
resource "pveguest_file" "management_keys" {
  node = var.node
  vmid = var.vmid
  kind = local.kind

  path    = local.management_keys_path
  content = "${join("\n", local.management_keys)}\n"
  mode    = "0600"
}

# ssh.service owns the TCP listener when socket activation is off.
resource "pveguest_systemd_unit" "ssh_socket" {
  node = var.node
  vmid = var.vmid
  kind = local.kind

  name    = "ssh.socket"
  enabled = false
  active  = false
}
