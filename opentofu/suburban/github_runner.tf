resource "proxmox_virtual_environment_container" "github_runner" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.github_runner.vmid

  depends_on = [
    proxmox_network_linux_bridge.trunk,
  ]

  initialization {
    hostname = var.github_runner.hostname
    ip_config {
      ipv6 {
        address = "${local.service_mapping.github_runner.ipv6}/64"
        gateway = var.github_runner.ipv6_gateway
      }
    }
    dns {
      servers = var.github_runner.dns_servers
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
    bridge      = var.github_runner.bridge
    mac_address = var.github_runner.mac_address
  }

  disk {
    datastore_id = var.github_runner.datastore_id
    size         = var.github_runner.disk_size_gb
  }

  memory {
    dedicated = var.github_runner.memory_mb
  }

  cpu {
    cores = var.github_runner.cpu_cores
  }

  tags = var.github_runner.tags

  operating_system {
    template_file_id = var.github_runner.template_file_id
    type             = "debian"
  }

  started       = true
  start_on_boot = true
  unprivileged  = true

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      initialization[0].user_account,
      operating_system[0].template_file_id,
    ]
  }
}
