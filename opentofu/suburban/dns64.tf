resource "proxmox_virtual_environment_container" "dns64_suburban" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.dns64_suburban.vmid

  depends_on = [
    proxmox_network_linux_bridge.trunk_suburban,
  ]

  initialization {
    hostname = local.service_mapping.dns64_suburban.hostname
    ip_config {
      ipv6 {
        address = "${local.service_mapping.dns64_suburban.ipv6}/64"
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

  # No features block. bind9 needs none of the advanced container features, and
  # Proxmox writes no features line when every flag is off, so declaring
  # nesting = false asserted a block the container never has. That mismatch made
  # the provider send fuse, keyctl and mknod on the first update, which Proxmox
  # refuses for anyone but root@pam.

  network_interface {
    name        = "eth0"
    bridge      = proxmox_network_linux_bridge.trunk_suburban.name
    mac_address = "BC:24:11:04:64:00"
  }

  disk {
    datastore_id = "local-zfs"
    size         = 4
  }

  memory {
    dedicated = 512
  }

  cpu {
    cores = 1
  }

  tags = ["lxc", "dns", "dns64"]

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
