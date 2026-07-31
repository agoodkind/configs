# Suburban testbed LXCs managed by OpenTofu.

resource "proxmox_virtual_environment_container" "mwan_failover_test" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.mwan_failover_test.vmid

  depends_on = [
    proxmox_network_linux_bridge.mwan_internal,
    proxmox_network_linux_bridge.isp_mbrains,
  ]

  initialization {
    hostname = "mwan-failover-test"
    ip_config {
      ipv4 {
        address = "dhcp"
      }
    }
    ip_config {
      ipv4 {
        address = "${local.service_mapping.mwan_failover_test.ipv4_transit}/29"
      }
      ipv6 {
        address = "${local.service_mapping.mwan_failover_test.ipv6_transit}/64"
      }
    }
  }

  features {
    nesting = true
  }

  network_interface {
    name        = "eth0"
    bridge      = "vmbr6"
    mac_address = "BC:24:11:E7:86:B4"
  }

  network_interface {
    name        = "eth1"
    bridge      = "vmbr2"
    mac_address = "BC:24:11:00:97:29"
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
  unprivileged  = false

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      operating_system[0].template_file_id,
    ]
  }
}

resource "proxmox_virtual_environment_container" "isp_webpass" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.isp_webpass.vmid

  depends_on = [
    proxmox_network_linux_bridge.isp_webpass,
    proxmox_network_linux_bridge.vm_management,
  ]

  initialization {
    hostname = "isp-webpass"
    dns {
      servers = ["2606:4700:4700::1111", "1.1.1.1"]
    }
    ip_config {
      ipv4 {
        address = "${local.service_mapping.isp_webpass.ipv4}/24"
      }
    }
    ip_config {
      ipv4 {
        address = "${local.service_mapping.isp_webpass.ipv4_uplink}/24"
        gateway = local.service_mapping.suburban_vmbr1.ipv4
      }
      ipv6 {
        address = "${local.service_mapping.isp_webpass.ipv6_uplink}/64"
        gateway = local.service_mapping.suburban_vmbr1.ipv6
      }
    }
  }

  features {
    nesting = true
  }

  network_interface {
    name        = "eth0"
    bridge      = "vmbr4"
    mac_address = "BC:24:11:7F:DE:4E"
  }

  network_interface {
    name        = "eth1"
    bridge      = proxmox_network_linux_bridge.vm_management.name
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

resource "proxmox_virtual_environment_container" "isp_att" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.isp_att.vmid

  depends_on = [
    proxmox_network_linux_bridge.isp_att,
    proxmox_network_linux_bridge.vm_management,
  ]

  initialization {
    hostname = "isp-att"
    dns {
      servers = ["2606:4700:4700::1111", "1.1.1.1"]
    }
    ip_config {
      ipv4 {
        address = "${local.service_mapping.isp_att.ipv4}/24"
      }
    }
    ip_config {
      ipv4 {
        address = "${local.service_mapping.isp_att.ipv4_uplink}/24"
        gateway = local.service_mapping.suburban_vmbr1.ipv4
      }
      ipv6 {
        address = "${local.service_mapping.isp_att.ipv6_uplink}/64"
        gateway = local.service_mapping.suburban_vmbr1.ipv6
      }
    }
  }

  features {
    nesting = true
  }

  network_interface {
    name        = "eth0"
    bridge      = "vmbr5"
    mac_address = "BC:24:11:D4:3C:A4"
  }

  network_interface {
    name        = "eth1"
    bridge      = proxmox_network_linux_bridge.vm_management.name
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

resource "proxmox_virtual_environment_container" "isp_mbrains" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.isp_mbrains.vmid

  depends_on = [
    proxmox_network_linux_bridge.isp_mbrains,
    proxmox_network_linux_bridge.vm_management,
  ]

  initialization {
    hostname = "isp-mbrains"
    dns {
      servers = ["2606:4700:4700::1111", "1.1.1.1"]
    }
    ip_config {
      ipv4 {
        address = "${local.service_mapping.isp_mbrains.ipv4}/24"
      }
      ipv6 {
        address = "${local.service_mapping.isp_mbrains.ipv6}/64"
      }
    }
    ip_config {
      ipv4 {
        address = "${local.service_mapping.isp_mbrains.ipv4_uplink}/24"
        gateway = local.service_mapping.suburban_vmbr1.ipv4
      }
      ipv6 {
        address = "${local.service_mapping.isp_mbrains.ipv6_uplink}/64"
        gateway = local.service_mapping.suburban_vmbr1.ipv6
      }
    }
  }

  features {
    nesting = true
  }

  network_interface {
    name        = "eth0"
    bridge      = "vmbr6"
    mac_address = "BC:24:11:87:1F:3A"
  }

  network_interface {
    name        = "eth1"
    bridge      = proxmox_network_linux_bridge.vm_management.name
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
