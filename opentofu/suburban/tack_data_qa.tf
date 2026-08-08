# Testbed three-node data tier and second app instance. Memory stays below
# production because the testbed hypervisor holds 31 GB total. The testbed
# validates topology and failover, not memory headroom.

resource "proxmox_virtual_environment_container" "tack_data1_suburban" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.tack_data1_suburban.vmid

  depends_on = [
    proxmox_network_linux_bridge.trunk_suburban,
  ]

  initialization {
    hostname = local.service_mapping.tack_data1_suburban.hostname
    ip_config {
      ipv6 {
        address = "${local.service_mapping.tack_data1_suburban.ipv6}/64"
        gateway = local.service_mapping.opnsense_suburban.ipv6_vmnet
      }
    }
    dns {
      servers = [local.service_mapping.dns64_suburban.ipv6]
    }
    user_account {
      keys = [var.ssh_keys]
    }
  }

  features {
    nesting = true
  }

  network_interface {
    name        = "eth0"
    bridge      = proxmox_network_linux_bridge.trunk_suburban.name
    mac_address = local.service_mapping.tack_data1_suburban.mac_address
  }

  disk {
    datastore_id = "local-zfs"
    size         = 40
  }

  memory {
    dedicated = 4096
  }

  cpu {
    cores = 2
  }

  tags = ["lxc", "tack", "tack-data", "qa", "docker"]

  operating_system {
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started      = true
  unprivileged = true

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      # Proxmox does not return injected SSH keys, so a re-import would read
      # the configured keys as an addition that forces replacement.
      initialization[0].user_account,
      operating_system[0].template_file_id,
    ]
  }
}

resource "proxmox_virtual_environment_container" "tack_data2_suburban" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.tack_data2_suburban.vmid

  depends_on = [
    proxmox_network_linux_bridge.trunk_suburban,
  ]

  initialization {
    hostname = local.service_mapping.tack_data2_suburban.hostname
    ip_config {
      ipv6 {
        address = "${local.service_mapping.tack_data2_suburban.ipv6}/64"
        gateway = local.service_mapping.opnsense_suburban.ipv6_vmnet
      }
    }
    dns {
      servers = [local.service_mapping.dns64_suburban.ipv6]
    }
    user_account {
      keys = [var.ssh_keys]
    }
  }

  features {
    nesting = true
  }

  network_interface {
    name        = "eth0"
    bridge      = proxmox_network_linux_bridge.trunk_suburban.name
    mac_address = local.service_mapping.tack_data2_suburban.mac_address
  }

  disk {
    datastore_id = "local-zfs"
    size         = 40
  }

  memory {
    dedicated = 4096
  }

  cpu {
    cores = 2
  }

  tags = ["lxc", "tack", "tack-data", "qa", "docker"]

  operating_system {
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started      = true
  unprivileged = true

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      # Proxmox does not return injected SSH keys, so a re-import would read
      # the configured keys as an addition that forces replacement.
      initialization[0].user_account,
      operating_system[0].template_file_id,
    ]
  }
}

resource "proxmox_virtual_environment_container" "tack_data3_suburban" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.tack_data3_suburban.vmid

  depends_on = [
    proxmox_network_linux_bridge.trunk_suburban,
  ]

  initialization {
    hostname = local.service_mapping.tack_data3_suburban.hostname
    ip_config {
      ipv6 {
        address = "${local.service_mapping.tack_data3_suburban.ipv6}/64"
        gateway = local.service_mapping.opnsense_suburban.ipv6_vmnet
      }
    }
    dns {
      servers = [local.service_mapping.dns64_suburban.ipv6]
    }
    user_account {
      keys = [var.ssh_keys]
    }
  }

  features {
    nesting = true
  }

  network_interface {
    name        = "eth0"
    bridge      = proxmox_network_linux_bridge.trunk_suburban.name
    mac_address = local.service_mapping.tack_data3_suburban.mac_address
  }

  disk {
    datastore_id = "local-zfs"
    size         = 40
  }

  memory {
    dedicated = 4096
  }

  cpu {
    cores = 2
  }

  tags = ["lxc", "tack", "tack-data", "qa", "docker"]

  operating_system {
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started      = true
  unprivileged = true

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      # Proxmox does not return injected SSH keys, so a re-import would read
      # the configured keys as an addition that forces replacement.
      initialization[0].user_account,
      operating_system[0].template_file_id,
    ]
  }
}

resource "proxmox_virtual_environment_container" "tack_app2_suburban" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.tack_app2_suburban.vmid

  depends_on = [
    proxmox_network_linux_bridge.trunk_suburban,
  ]

  initialization {
    hostname = local.service_mapping.tack_app2_suburban.hostname
    ip_config {
      ipv6 {
        address = "${local.service_mapping.tack_app2_suburban.ipv6}/64"
        gateway = local.service_mapping.opnsense_suburban.ipv6_vmnet
      }
    }
    dns {
      servers = [local.service_mapping.dns64_suburban.ipv6]
    }
    user_account {
      keys = [var.ssh_keys]
    }
  }

  features {
    nesting = true
  }

  network_interface {
    name        = "eth0"
    bridge      = proxmox_network_linux_bridge.trunk_suburban.name
    mac_address = local.service_mapping.tack_app2_suburban.mac_address
  }

  disk {
    datastore_id = "local-zfs"
    size         = 30
  }

  memory {
    dedicated = 2048
  }

  cpu {
    cores = 2
  }

  tags = ["lxc", "tack", "tack-app", "qa", "docker"]

  operating_system {
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started      = true
  unprivileged = true

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      # Proxmox does not return injected SSH keys, so a re-import would read
      # the configured keys as an addition that forces replacement.
      initialization[0].user_account,
      operating_system[0].template_file_id,
    ]
  }
}
