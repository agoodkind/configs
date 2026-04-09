data "http" "github_ssh_keys" {
  url = "https://github.com/${var.github_ssh_keys_user}.keys"
}

# Plane project management LXC (VMID 115)
# Runs Docker CE + docker compose on Debian 13 (Trixie).
# Nesting is required for Docker's overlay filesystem inside LXC.
resource "proxmox_virtual_environment_container" "plane" {
  node_name = "vault"
  vm_id     = 115

  initialization {
    hostname = "plane.home.goodkind.io"
    ip_config {
      ipv6 {
        address = "3d06:bad:b01::115/64"
        gateway = "3d06:bad:b01::1"
      }
    }
    user_account {
      keys = [trimspace(data.http.github_ssh_keys.response_body)]
    }
  }

  features {
    nesting = true
  }

  network_interface {
    name        = "eth0"
    bridge      = "vmbr0"
    mac_address = "BC:24:11:A3:52:01"
  }

  disk {
    datastore_id = "local-lvm"
    size         = 25
  }

  memory { dedicated = 4096 }
  cpu    { cores     = 2 }

  tags = ["lxc", "plane", "docker"]

  operating_system {
    template_file_id = "storage:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started      = true
  unprivileged = true

  lifecycle {
    prevent_destroy = true
  }
}

# Tack project management LXC (VMID 117)
# Runs Docker CE + docker compose on Debian 13 (Trixie).
# Nesting is required for Docker's overlay filesystem inside LXC.
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
      keys = [trimspace(data.http.github_ssh_keys.response_body)]
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

  memory { dedicated = 2048 }
  cpu    { cores     = 2 }

  tags = ["lxc", "tack", "docker"]

  operating_system {
    template_file_id = "storage:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started      = true
  unprivileged = true

  lifecycle {
    prevent_destroy = true
  }
}
