resource "proxmox_virtual_environment_container" "seaweedfs_suburban" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.seaweedfs_suburban.vmid

  depends_on = [
    proxmox_network_linux_bridge.trunk_suburban,
  ]

  initialization {
    hostname = local.service_mapping.seaweedfs_suburban.hostname
    ip_config {
      ipv6 {
        address = "${local.service_mapping.seaweedfs_suburban.ipv6}/64"
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
    mac_address = "BC:24:11:04:10:00"
  }

  # The object store's IO is asynchronous backup traffic, so its disk lives on
  # the slow BX500 mirror rather than rpool (TACK-495).
  disk {
    datastore_id = "slow-zfs"
    size         = 100
  }

  memory {
    dedicated = 4096
  }

  cpu {
    cores = 2
  }

  tags = ["lxc", "seaweedfs", "s3"]

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
      # The provider replaces a container to change its disk's datastore and
      # has no volume move. The disk moves with pct move-volume on the
      # hypervisor instead, and OpenTofu never plans the change itself.
      disk[0].datastore_id,
    ]
  }
}
