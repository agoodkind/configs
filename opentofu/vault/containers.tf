# Tack project management LXC on vault (VMID 117).

resource "proxmox_virtual_environment_container" "tack" {
  node_name = "vault"
  vm_id     = 117

  initialization {
    hostname = "tack.home.goodkind.io"
    ip_config {
      ipv6 {
        address = "3d06:bad:b01::117/64"
        gateway = "3d06:bad:b01::1"
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
    bridge      = "vmbr0"
    mac_address = "BC:24:11:A3:52:17"
  }

  disk {
    datastore_id = "local-lvm"
    size         = 40
  }

  memory {
    dedicated = 8192
  }

  cpu {
    cores = 2
  }

  tags = ["lxc", "tack", "docker"]

  operating_system {
    template_file_id = "storage:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started       = true
  start_on_boot = true
  unprivileged  = true

  lifecycle {
    prevent_destroy = true
  }
}
