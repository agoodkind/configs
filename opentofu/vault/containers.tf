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

# Consul service-discovery LXC on vault (VMID 106).
resource "proxmox_virtual_environment_container" "consul" {
  node_name = "vault"
  vm_id     = 106

  initialization {
    hostname = "consul.home.goodkind.io"
    ip_config {
      ipv6 {
        address = "3d06:bad:b01::106/64"
        gateway = "3d06:bad:b01::1"
      }
    }
  }

  network_interface {
    name        = "eth0"
    bridge      = "vmbr0"
    mac_address = "BC:24:11:4A:03:7B"
  }

  disk {
    datastore_id = "local-lvm"
    size         = 4
  }

  memory {
    dedicated = 512
    swap      = 512
  }

  cpu {
    architecture = "amd64"
    cores        = 1
    limit        = 0
  }

  console {
    enabled   = true
    tty_count = 2
    type      = "tty"
  }

  tags = ["consul", "lxc"]

  operating_system {
    template_file_id = "storage:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "ubuntu"
  }

  started       = true
  start_on_boot = true
  unprivileged  = true

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      operating_system[0].template_file_id,
    ]
  }
}

# Minecraft LXC on vault (VMID 109).
resource "proxmox_virtual_environment_container" "mc" {
  node_name = "vault"
  vm_id     = 109

  initialization {
    hostname = "mc.home.goodkind.io"
    ip_config {
      ipv6 {
        address = "3d06:bad:b01::109/64"
        gateway = "3d06:bad:b01::1"
      }
    }
  }

  network_interface {
    name        = "eth0"
    bridge      = "vmbr0"
    mac_address = "BC:24:11:16:C7:47"
  }

  disk {
    datastore_id = "local-lvm"
    size         = 20
  }

  memory {
    dedicated = 8192
    swap      = 512
  }

  cpu {
    architecture = "amd64"
    cores        = 4
    limit        = 0
  }

  console {
    enabled   = true
    tty_count = 2
    type      = "tty"
  }

  tags = ["lxc", "mc", "minecraft"]

  operating_system {
    template_file_id = "storage:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "ubuntu"
  }

  started       = true
  start_on_boot = true
  unprivileged  = true

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      operating_system[0].template_file_id,
    ]
  }
}

# AdGuard Home LXC on vault (VMID 112).
resource "proxmox_virtual_environment_container" "adguard" {
  node_name = "vault"
  vm_id     = 112

  initialization {
    hostname = "adguard.home.goodkind.io"
    ip_config {
      ipv6 {
        address = "3d06:bad:b01::53/64"
        gateway = "3d06:bad:b01::1"
      }
    }
  }

  features {
    nesting = true
  }

  network_interface {
    name        = "eth0"
    bridge      = "vmbr0"
    mac_address = "bc:24:11:ee:f9:50"
  }

  disk {
    datastore_id = "local-lvm"
    size         = 8
  }

  memory {
    dedicated = 2048
    swap      = 512
  }

  cpu {
    architecture = "amd64"
    cores        = 2
    limit        = 0
  }

  console {
    enabled   = true
    tty_count = 2
    type      = "tty"
  }

  tags = ["adguard", "legacyv4", "lxc"]

  operating_system {
    template_file_id = "storage:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "ubuntu"
  }

  started       = true
  start_on_boot = true
  unprivileged  = true

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      operating_system[0].template_file_id,
    ]
  }
}
