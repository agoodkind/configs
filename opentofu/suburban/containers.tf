# Suburban testbed LXCs managed by OpenTofu.

resource "proxmox_virtual_environment_container" "mwan_failover_test" {
  node_name = "hypervisor"
  vm_id     = 100

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
      ipv6 {
        address = "auto"
      }
    }
    ip_config {
      ipv4 {
        address = "10.250.250.4/29"
      }
      ipv6 {
        address = "3d06:bad:b01:201::4/64"
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
  vm_id     = 200

  depends_on = [
    proxmox_network_linux_bridge.isp_webpass,
  ]

  initialization {
    hostname = "isp-webpass"
    ip_config {
      ipv4 {
        address = "10.240.204.1/24"
      }
    }
    ip_config {
      ipv4 {
        address = "dhcp"
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
    bridge      = "vmbr0"
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
  vm_id     = 201

  depends_on = [
    proxmox_network_linux_bridge.isp_att,
  ]

  initialization {
    hostname = "isp-att"
    ip_config {
      ipv4 {
        address = "10.240.205.1/24"
      }
    }
    ip_config {
      ipv4 {
        address = "dhcp"
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
    bridge      = "vmbr0"
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
  vm_id     = 202

  depends_on = [
    proxmox_network_linux_bridge.isp_mbrains,
  ]

  initialization {
    hostname = "isp-mbrains"
    ip_config {
      ipv4 {
        address = "10.240.206.1/24"
      }
      ipv6 {
        address = "3d06:bad:b01:250::1/64"
      }
    }
    ip_config {
      ipv4 {
        address = "dhcp"
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
    bridge      = "vmbr0"
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
