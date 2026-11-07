# Router-2 simulator: an FRR speaker on the transit link that proves router
# N+1 onboarding needs no MWAN change. One interface, on the transit bridge;
# announced-prefix.service adds the announced prefix to the loopback.
resource "proxmox_virtual_environment_container" "router2_suburban" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.router2_suburban.vmid


  initialization {
    hostname = local.service_mapping.router2_suburban.hostname
    # Public resolvers, matching the ISP sims. The transit link is the
    # router's WAN side, so this guest cannot reach the guest-segment
    # resolver through it.
    dns {
      servers = ["2606:4700:4700::1111", "1.1.1.1"]
    }
    # Transit link, where it peers BGP. No gateway here: the router does not
    # forward this guest to the guest segment or the internet, so egress
    # rides the management uplink below.
    ip_config {
      ipv4 {
        address = "${local.service_mapping.router2_suburban.ipv4_transit}/29"
      }
      ipv6 {
        address = "${local.service_mapping.router2_suburban.ipv6_transit}/64"
      }
    }
    ip_config {
      ipv4 {
        address = "${local.service_mapping.router2_suburban.ipv4_uplink}/24"
        gateway = local.service_mapping.vmbr1_suburban.ipv4
      }
      ipv6 {
        address = "${local.service_mapping.router2_suburban.ipv6_uplink}/64"
        gateway = local.service_mapping.vmbr1_suburban.ipv6
      }
    }
    user_account {
      keys = [var.ssh_keys]
    }
  }

  # No features block. FRR needs none of the advanced container features, and
  # Proxmox writes no features line when every flag is off. Declaring one here
  # makes the provider send fuse, keyctl and mknod on create, which Proxmox
  # refuses for any actor but root@pam on a privileged container.

  network_interface {
    name        = "eth0"
    bridge      = var.host_networks.mwan_suburban
    mac_address = "BC:24:11:02:09:05"
  }

  network_interface {
    name        = "eth1"
    bridge      = var.host_networks.vm_management_suburban
    mac_address = "BC:24:11:02:09:93"
  }

  disk {
    datastore_id = "local-zfs"
    size         = 4
  }

  memory {
    dedicated = 512
    swap      = 512
  }

  cpu {
    architecture = "amd64"
    cores        = 1
    limit        = 0
  }

  console {
    enabled   = true
    tty_count = 2
    type      = "tty"
  }

  tags = []

  operating_system {
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started       = true
  start_on_boot = true
  unprivileged  = true

  # The simulator does not store state worth protecting. This module writes
  # the FRR configuration and the loopback unit, and one apply restores a
  # recreated guest.
  lifecycle {
    prevent_destroy = false
    ignore_changes = [
      initialization[0].user_account,
      operating_system[0].template_file_id,
    ]
  }
}

locals {
  router2_kind = "lxc"
  router2      = local.service_mapping.router2_suburban

  # The simulator peers with the transit address of each MWAN speaker.
  router2_neighbors = [
    local.service_mapping.mwan_suburban.ipv6_transit,
    local.service_mapping.mwan_failover_suburban.ipv6_transit,
  ]

  # announced-prefix.service adds the first address of the announced /64 to lo.
  router2_loopback_address = "${cidrhost(local.router2.announced_prefix_v6, 1)}/${split("/", local.router2.announced_prefix_v6)[1]}"
}

resource "pveguest_apt_packages" "router2" {
  node = local.router2.node
  vmid = local.router2.vmid
  kind = local.router2_kind

  packages = toset(["frr"])

  depends_on = [proxmox_virtual_environment_container.router2_suburban]
}

resource "pveguest_file" "announced_prefix" {
  node = local.router2.node
  vmid = local.router2.vmid
  kind = local.router2_kind

  path = "/etc/systemd/system/announced-prefix.service"
  content = templatefile("${path.module}/files/announced-prefix.service.tftpl", {
    loopback_address = local.router2_loopback_address
  })

  depends_on = [proxmox_virtual_environment_container.router2_suburban]
}

resource "pveguest_systemd_unit" "announced_prefix" {
  node = local.router2.node
  vmid = local.router2.vmid
  kind = local.router2_kind

  name    = "announced-prefix.service"
  enabled = true
  active  = true

  restart_on = {
    unit = pveguest_file.announced_prefix.write_id
  }
}

resource "pveguest_file" "frr_daemons" {
  node = local.router2.node
  vmid = local.router2.vmid
  kind = local.router2_kind

  path    = "/etc/frr/daemons"
  content = file("${path.module}/../routing/files/frr-daemons")
  mode    = "0640"
  owner   = "frr"
  group   = "frr"

  depends_on = [pveguest_apt_packages.router2]
}

resource "pveguest_file" "frr_conf" {
  node = local.router2.node
  vmid = local.router2.vmid
  kind = local.router2_kind

  path = "/etc/frr/frr.conf"
  content = templatefile("${path.module}/files/frr.conf.tftpl", {
    hostname         = split(".", local.router2.hostname)[0]
    asn              = var.bgp_asn
    router_id        = local.router2.ipv4_transit
    neighbors        = local.router2_neighbors
    announced_prefix = local.router2.announced_prefix_v6
  })
  mode     = "0640"
  owner    = "frr"
  group    = "frr"
  validate = "vtysh -C -f %s"

  depends_on = [pveguest_apt_packages.router2]
}

resource "pveguest_systemd_unit" "frr" {
  node = local.router2.node
  vmid = local.router2.vmid
  kind = local.router2_kind

  name    = "frr.service"
  enabled = true
  active  = true

  restart_on = {
    daemons = pveguest_file.frr_daemons.write_id
    conf    = pveguest_file.frr_conf.write_id
  }

  depends_on = [pveguest_systemd_unit.announced_prefix]
}

variable "service_mapping" { type = object({
  router2_suburban = object({
    node                = string
    vmid                = number
    hostname            = string
    ipv4_transit        = string
    ipv6_transit        = string
    ipv4_uplink         = string
    ipv6_uplink         = string
    announced_prefix_v6 = string
  })
  mwan_suburban = object({
    ipv6_transit = string
  })
  mwan_failover_suburban = object({
    ipv6_transit = string
  })
  vmbr1_suburban = object({
    ipv4 = string
    ipv6 = string
  })
}) }

variable "ssh_keys" { type = string }

variable "host_networks" { type = map(string) }

variable "bgp_asn" {
  description = "The bgp_asn variable specifies the iBGP AS that the simulator shares with the MWAN speakers."
  type        = number
}

locals { service_mapping = var.service_mapping }

terraform {
  required_providers {
    proxmox = {
      source  = "bpg/proxmox"
      version = ">= 0.106.0"

    }
    pveguest = {
      source  = "tofu.home.arpa/agoodkind/pveguest"
      version = ">= 0.1.3"
    }
  }
}
