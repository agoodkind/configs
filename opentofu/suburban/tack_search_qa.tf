resource "proxmox_virtual_environment_container" "tack_search1_suburban" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.tack_search1_suburban.vmid

  depends_on = [
    proxmox_network_linux_bridge.trunk_suburban,
  ]

  initialization {
    hostname = local.service_mapping.tack_search1_suburban.hostname
    ip_config {
      ipv6 {
        address = "${local.service_mapping.tack_search1_suburban.ipv6}/64"
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
    mac_address = local.service_mapping.tack_search1_suburban.mac_address
  }

  disk {
    datastore_id = "local-zfs"
    size          = 40
    mount_options = ["discard"]
  }

  memory {
    dedicated = 8192
  }

  cpu {
    cores = 2
  }

  tags = ["lxc", "tack", "qa", "search", "docker"]

  operating_system {
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started      = true
  unprivileged = true

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      initialization[0].user_account,
      operating_system[0].template_file_id,
      mount_point,
    ]
  }
}
