resource "proxmox_virtual_environment_container" "dns64" {
  node_name = "hypervisor"
  vm_id     = var.dns64.vm_id

  depends_on = [
    proxmox_network_linux_bridge.trunk,
  ]

  initialization {
    hostname = var.dns64.hostname
    ip_config {
      ipv6 {
        address = var.dns64.ipv6_address
        gateway = var.dns64.ipv6_gateway
      }
    }
    user_account {
      keys = [var.ssh_keys]
    }
  }

  features {
    nesting = false
  }

  network_interface {
    name        = "eth0"
    bridge      = var.dns64.bridge
    mac_address = var.dns64.mac_address
  }

  disk {
    datastore_id = var.dns64.datastore_id
    size         = var.dns64.disk_size_gb
  }

  memory {
    dedicated = var.dns64.memory_mb
  }

  cpu {
    cores = var.dns64.cpu_cores
  }

  tags = var.dns64.tags

  operating_system {
    template_file_id = var.dns64.template_file_id
    type             = "debian"
  }

  started      = true
  unprivileged = true

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      operating_system[0].template_file_id,
    ]
  }
}
