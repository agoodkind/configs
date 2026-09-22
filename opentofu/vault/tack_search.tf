resource "proxmox_virtual_environment_container" "tack_search1" {
  node_name = "vault"
  vm_id     = local.service_mapping.tack_search1.vmid

  initialization {
    hostname = local.service_mapping.tack_search1.hostname
    ip_config {
      ipv6 {
        address = "${local.service_mapping.tack_search1.ipv6}/64"
        gateway = local.service_mapping.opnsense.ipv6
      }
    }
    dns {
      servers = [local.service_mapping.dns64.ipv6]
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
    bridge      = "vmbr0"
    mac_address = local.service_mapping.tack_search1.mac_address
  }

  disk {
    datastore_id = "local-lvm"
    size          = 40
    mount_options = ["discard"]
  }

  memory {
    dedicated = 8192
  }

  cpu {
    cores = 2
  }

  tags = ["lxc", "tack", "search", "docker"]

  operating_system {
    template_file_id = "storage:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
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
