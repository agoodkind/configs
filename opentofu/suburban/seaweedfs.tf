resource "proxmox_virtual_environment_container" "seaweedfs" {
  node_name = "hypervisor"
  vm_id     = var.seaweedfs.vm_id

  depends_on = [
    proxmox_network_linux_bridge.trunk,
  ]

  initialization {
    hostname = var.seaweedfs.hostname
    ip_config {
      ipv6 {
        address = var.seaweedfs.ipv6_address
        gateway = var.seaweedfs.ipv6_gateway
      }
    }
    dns {
      servers = var.seaweedfs.dns_servers
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
    bridge      = var.seaweedfs.bridge
    mac_address = var.seaweedfs.mac_address
  }

  disk {
    datastore_id = var.seaweedfs.datastore_id
    size         = var.seaweedfs.disk_size_gb
  }

  memory {
    dedicated = var.seaweedfs.memory_mb
  }

  cpu {
    cores = var.seaweedfs.cpu_cores
  }

  tags = var.seaweedfs.tags

  operating_system {
    template_file_id = var.seaweedfs.template_file_id
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
