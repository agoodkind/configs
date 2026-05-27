# Tack QA LXC on suburban. All values come from var.tack_qa so the module's
# default block is the single source of truth for the resource shape.

resource "proxmox_virtual_environment_container" "tack_qa" {
  node_name = "hypervisor"
  vm_id     = var.tack_qa.vm_id

  depends_on = [
    proxmox_network_linux_bridge.vm_management,
  ]

  initialization {
    hostname = var.tack_qa.hostname
    ip_config {
      ipv6 {
        address = var.tack_qa.ipv6_address
        gateway = var.tack_qa.ipv6_gateway
      }
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
    bridge      = var.tack_qa.bridge
    mac_address = var.tack_qa.mac_address
  }

  disk {
    datastore_id = var.tack_qa.datastore_id
    size         = var.tack_qa.disk_size_gb
  }

  memory {
    dedicated = var.tack_qa.memory_mb
  }

  cpu {
    cores = var.tack_qa.cpu_cores
  }

  tags = var.tack_qa.tags

  operating_system {
    template_file_id = var.tack_qa.template_file_id
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
