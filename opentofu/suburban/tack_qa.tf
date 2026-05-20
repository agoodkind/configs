# Tack QA LXC on suburban (VMID 103).

resource "proxmox_virtual_environment_container" "tack_qa" {
  node_name = "hypervisor"
  vm_id     = var.tack_qa_vm_id

  depends_on = [
    proxmox_network_linux_bridge.vm_management,
  ]

  initialization {
    hostname = var.tack_qa_hostname
    ip_config {
      ipv6 {
        address = var.tack_qa_ipv6_address
        gateway = var.tack_qa_ipv6_gateway
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
    bridge      = var.tack_qa_bridge
    mac_address = var.tack_qa_mac_address
  }

  disk {
    datastore_id = var.tack_qa_datastore_id
    size         = var.tack_qa_disk_size_gb
  }

  memory {
    dedicated = var.tack_qa_memory_mb
  }

  cpu {
    cores = var.tack_qa_cpu_cores
  }

  tags = var.tack_qa_tags

  operating_system {
    template_file_id = var.tack_qa_template_file_id
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
