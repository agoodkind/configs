# Tack project management LXC on vault.

resource "proxmox_virtual_environment_container" "tack" {
  node_name = "vault"
  vm_id     = local.service_mapping.tack.vmid

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

  # Sized to match the live container. It was grown in place, and Proxmox cannot
  # shrink a container disk, so understating these here makes a plan propose a
  # shrink that either fails or damages the store.
  disk {
    datastore_id = "local-lvm"
    size         = 300
  }

  memory {
    dedicated = 16384
  }

  cpu {
    cores = 6
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
    ignore_changes = [
      # Proxmox does not return injected SSH keys, and the template name is not
      # stored in pct config, so both read as changes that force replacement.
      initialization[0].user_account,
      operating_system[0].template_file_id,
    ]
  }
}

# Minecraft LXC on vault (VMID 109).
resource "proxmox_virtual_environment_container" "mc" {
  node_name = "vault"
  vm_id     = local.service_mapping.mc.vmid

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
  vm_id     = local.service_mapping.adguard.vmid

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

# Internal-only SeaweedFS object-store LXC on vault. The production
# twin of the suburban CT 410 store, on the same prod VMNET segment as the tack
# LXC so the prod tack host reaches its S3 endpoint without crossing segments.
# Runs the weed binary under systemd (deploy-seaweedfs.yml); never exposed off
# the segment. Backup destination for the prod tack stores.
resource "proxmox_virtual_environment_container" "seaweedfs" {
  node_name = "vault"
  vm_id     = local.service_mapping.seaweedfs.vmid

  initialization {
    hostname = "seaweedfs.home.goodkind.io"
    ip_config {
      ipv6 {
        address = "3d06:bad:b01::118/64"
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
    mac_address = "BC:24:11:00:01:18"
  }

  disk {
    datastore_id = "local-lvm"
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
