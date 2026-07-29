resource "proxmox_virtual_environment_container" "dns64" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.dns64_suburban.vmid

  depends_on = [
    proxmox_network_linux_bridge.trunk,
  ]

  initialization {
    hostname = var.dns64.hostname
    ip_config {
      ipv6 {
        address = "${local.service_mapping.dns64_suburban.ipv6}/64"
        gateway = var.dns64.ipv6_gateway
      }
    }
    dns {
      servers = var.dns64.dns_servers
    }
    user_account {
      keys = [var.ssh_keys]
    }
  }

  # No features block. bind9 needs none of the advanced container features, and
  # Proxmox writes no features line when every flag is off, so declaring
  # nesting = false asserted a block the container never has. That mismatch made
  # the provider send fuse, keyctl and mknod on the first update, which Proxmox
  # refuses for anyone but root@pam.

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
      # Proxmox does not return injected SSH keys, so a re-import would read
      # the configured keys as an addition that forces replacement.
      initialization[0].user_account,
      operating_system[0].template_file_id,
    ]
  }
}
