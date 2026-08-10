# Suburban testbed LXCs managed by OpenTofu.

resource "proxmox_virtual_environment_container" "mwan_failover_suburban" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.mwan_failover_suburban.vmid

  depends_on = [
    proxmox_network_linux_bridge.mwan_suburban,
    proxmox_network_linux_bridge.isp_mbrains_suburban,
    proxmox_network_linux_bridge.trunk_suburban,
  ]

  initialization {
    hostname = local.service_mapping.mwan_failover_suburban.hostname
    ip_config {
      ipv4 {
        address = "dhcp"
      }
    }
    ip_config {
      ipv4 {
        address = "${local.service_mapping.mwan_failover_suburban.ipv4_transit}/29"
      }
      ipv6 {
        address = "${local.service_mapping.mwan_failover_suburban.ipv6_transit}/64"
      }
    }
    # Guest segment. Deploys target this address, so it carries no gateway:
    # the ISP-sim default route on eth0 is what this guest uses for egress,
    # and a second default would fight it.
    ip_config {
      ipv4 {
        address = "${local.service_mapping.mwan_failover_suburban.ipv4_vmnet}/24"
      }
      ipv6 {
        address = "${local.service_mapping.mwan_failover_suburban.ipv6_vmnet}/64"
      }
    }
    user_account {
      keys = [var.ssh_keys]
    }
  }

  # No features block. The failover runs systemd, the mwan binary, and
  # nftables, none of which need container features, and Proxmox refuses
  # feature-flag writes from non-root@pam actors on privileged containers.

  network_interface {
    name        = "eth0"
    bridge      = proxmox_network_linux_bridge.isp_mbrains_suburban.name
    mac_address = "BC:24:11:E7:86:B4"
  }

  network_interface {
    name        = "eth1"
    bridge      = proxmox_network_linux_bridge.mwan_suburban.name
    mac_address = "BC:24:11:00:97:29"
  }

  network_interface {
    name        = "eth2"
    bridge      = proxmox_network_linux_bridge.trunk_suburban.name
    mac_address = "BC:24:11:04:21:60"
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

  lifecycle {
    prevent_destroy = false
    ignore_changes = [
      operating_system[0].template_file_id,
    ]
  }
}

resource "proxmox_virtual_environment_container" "isp_webpass_suburban" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.isp_webpass_suburban.vmid

  depends_on = [
    proxmox_network_linux_bridge.isp_webpass_suburban,
    proxmox_network_linux_bridge.vm_management_suburban,
  ]

  initialization {
    hostname = local.service_mapping.isp_webpass_suburban.hostname
    dns {
      servers = ["2606:4700:4700::1111", "1.1.1.1"]
    }
    ip_config {
      ipv4 {
        address = "${local.service_mapping.isp_webpass_suburban.ipv4}/24"
      }
    }
    ip_config {
      ipv4 {
        address = "${local.service_mapping.isp_webpass_suburban.ipv4_uplink}/24"
        gateway = local.service_mapping.vmbr1_suburban.ipv4
      }
      ipv6 {
        address = "${local.service_mapping.isp_webpass_suburban.ipv6_uplink}/64"
        gateway = local.service_mapping.vmbr1_suburban.ipv6
      }
    }
  }

  features {
    nesting = true
  }

  network_interface {
    name        = "eth0"
    bridge      = proxmox_network_linux_bridge.isp_webpass_suburban.name
    mac_address = "BC:24:11:7F:DE:4E"
  }

  network_interface {
    name        = "eth1"
    bridge      = proxmox_network_linux_bridge.vm_management_suburban.name
    mac_address = "BC:24:11:FC:17:A7"
  }

  disk {
    datastore_id = "local-zfs"
    size         = 2
  }

  memory {
    dedicated = 128
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
  unprivileged  = false

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      operating_system[0].template_file_id,
    ]
  }
}

resource "proxmox_virtual_environment_container" "isp_att_suburban" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.isp_att_suburban.vmid

  depends_on = [
    proxmox_network_linux_bridge.isp_att_suburban,
    proxmox_network_linux_bridge.vm_management_suburban,
  ]

  initialization {
    hostname = local.service_mapping.isp_att_suburban.hostname
    dns {
      servers = ["2606:4700:4700::1111", "1.1.1.1"]
    }
    ip_config {
      ipv4 {
        address = "${local.service_mapping.isp_att_suburban.ipv4}/24"
      }
    }
    ip_config {
      ipv4 {
        address = "${local.service_mapping.isp_att_suburban.ipv4_uplink}/24"
        gateway = local.service_mapping.vmbr1_suburban.ipv4
      }
      ipv6 {
        address = "${local.service_mapping.isp_att_suburban.ipv6_uplink}/64"
        gateway = local.service_mapping.vmbr1_suburban.ipv6
      }
    }
  }

  features {
    nesting = true
  }

  network_interface {
    name        = "eth0"
    bridge      = proxmox_network_linux_bridge.isp_att_suburban.name
    mac_address = "BC:24:11:D4:3C:A4"
  }

  network_interface {
    name        = "eth1"
    bridge      = proxmox_network_linux_bridge.vm_management_suburban.name
    mac_address = "BC:24:11:6C:B8:2B"
  }

  disk {
    datastore_id = "local-zfs"
    size         = 2
  }

  memory {
    dedicated = 128
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
  unprivileged  = false

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      operating_system[0].template_file_id,
    ]
  }
}

resource "proxmox_virtual_environment_container" "isp_mbrains_suburban" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.isp_mbrains_suburban.vmid

  depends_on = [
    proxmox_network_linux_bridge.isp_mbrains_suburban,
    proxmox_network_linux_bridge.vm_management_suburban,
  ]

  initialization {
    hostname = local.service_mapping.isp_mbrains_suburban.hostname
    dns {
      servers = ["2606:4700:4700::1111", "1.1.1.1"]
    }
    ip_config {
      ipv4 {
        address = "${local.service_mapping.isp_mbrains_suburban.ipv4}/24"
      }
      ipv6 {
        address = "${local.service_mapping.isp_mbrains_suburban.ipv6}/64"
      }
    }
    ip_config {
      ipv4 {
        address = "${local.service_mapping.isp_mbrains_suburban.ipv4_uplink}/24"
        gateway = local.service_mapping.vmbr1_suburban.ipv4
      }
      ipv6 {
        address = "${local.service_mapping.isp_mbrains_suburban.ipv6_uplink}/64"
        gateway = local.service_mapping.vmbr1_suburban.ipv6
      }
    }
  }

  features {
    nesting = true
  }

  network_interface {
    name        = "eth0"
    bridge      = proxmox_network_linux_bridge.isp_mbrains_suburban.name
    mac_address = "BC:24:11:87:1F:3A"
  }

  network_interface {
    name        = "eth1"
    bridge      = proxmox_network_linux_bridge.vm_management_suburban.name
    mac_address = "BC:24:11:DF:62:D3"
  }

  disk {
    datastore_id = "local-zfs"
    size         = 2
  }

  memory {
    dedicated = 128
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
  unprivileged  = false

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      operating_system[0].template_file_id,
    ]
  }
}

# Router-2 simulator: an FRR speaker on the transit link that proves router
# N+1 onboarding needs no MWAN change. One interface, on the transit bridge;
# the announced prefix lives on its loopback, configured by
# deploy-router2-sim.yml through pct alongside FRR itself.
resource "proxmox_virtual_environment_container" "router2_suburban" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.router2_suburban.vmid

  depends_on = [
    proxmox_network_linux_bridge.mwan_suburban,
    proxmox_network_linux_bridge.vm_management_suburban,
  ]

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
    bridge      = proxmox_network_linux_bridge.mwan_suburban.name
    mac_address = "BC:24:11:02:09:05"
  }

  network_interface {
    name        = "eth1"
    bridge      = proxmox_network_linux_bridge.vm_management_suburban.name
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

  # The simulator holds no state worth protecting: its FRR config is rendered
  # by its deploy play, so recreating it costs one deploy.
  lifecycle {
    prevent_destroy = false
    ignore_changes = [
      operating_system[0].template_file_id,
    ]
  }
}
