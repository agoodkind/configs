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
    dedicated = 24576
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

# Developer sandbox LXC on vault. Ansible does not manage its inside.
resource "proxmox_virtual_environment_container" "debianct" {
  node_name = "vault"
  vm_id     = local.service_mapping.debianct.vmid

  initialization {
    hostname = "debianct.home.goodkind.io"
    ip_config {
      ipv6 {
        address = "${local.service_mapping.debianct.ipv6}/64"
        gateway = "3d06:bad:b01::1"
      }
    }
  }

  features {
    nesting = true
    fuse    = true
  }

  network_interface {
    name        = "eth0"
    bridge      = "vmbr0"
    mac_address = "BC:24:11:73:B0:3C"
  }

  disk {
    datastore_id = "local-lvm"
    size         = 82
  }

  memory {
    dedicated = 16384
    swap      = 16384
  }

  cpu {
    architecture = "amd64"
    cores        = 8
  }

  console {
    enabled   = true
    tty_count = 2
    type      = "tty"
  }

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
      operating_system[0].template_file_id,
    ]
  }
}

# UniFi network controller LXC on vault. Created by a community script that
# set a decorative description and raw TUN passthrough lines; both stay
# unmanaged.
resource "proxmox_virtual_environment_container" "unifi" {
  node_name = "vault"
  vm_id     = local.service_mapping.unifi.vmid

  initialization {
    hostname = "unifi.home.goodkind.io"
    ip_config {
      ipv4 {
        address = "${local.service_mapping.unifi.ipv4}/32"
        gateway = "10.250.0.1"
      }
      ipv6 {
        address = "${local.service_mapping.unifi.ipv6}/64"
        gateway = "3d06:bad:b01::1"
      }
    }
  }

  features {
    nesting = true
    fuse    = true
    keyctl  = true
  }

  network_interface {
    name        = "eth0"
    bridge      = "vmbr0"
    mac_address = "BC:24:11:41:36:D5"
  }

  disk {
    datastore_id = "local-lvm"
    size         = 16
  }

  memory {
    dedicated = 2048
    swap      = 512
  }

  cpu {
    architecture = "amd64"
    cores        = 2
  }

  tags = ["community-script", "controller", "legacyv4", "network", "unifi"]

  console {
    enabled   = true
    tty_count = 2
    type      = "tty"
  }

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
      description,
      operating_system[0].template_file_id,
    ]
  }
}

# DNS64 resolver LXC on vault.
resource "proxmox_virtual_environment_container" "dns64" {
  node_name = "vault"
  vm_id     = local.service_mapping.dns64.vmid

  initialization {
    hostname = "dns64.home.goodkind.io"
    ip_config {
      ipv6 {
        address = "${local.service_mapping.dns64.ipv6}/64"
        gateway = "3d06:bad:b01::1"
      }
    }
  }

  network_interface {
    name        = "eth0"
    bridge      = "vmbr0"
    mac_address = "BC:24:11:7E:34:23"
  }

  disk {
    datastore_id = "local-lvm"
    size         = 4
  }

  memory {
    dedicated = 256
    swap      = 256
  }

  cpu {
    architecture = "amd64"
    cores        = 1
  }

  tags = ["dns64", "lxc"]

  console {
    enabled   = true
    tty_count = 2
    type      = "tty"
  }

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

# Reverse proxy LXC on vault (Traefik, SSHPiper, cloudflared).
resource "proxmox_virtual_environment_container" "proxy" {
  node_name = "vault"
  vm_id     = local.service_mapping.proxy.vmid

  initialization {
    hostname = "proxy.home.goodkind.io"
    ip_config {
      ipv4 {
        address = "${local.service_mapping.proxy.ipv4}/32"
        gateway = "10.250.0.1"
      }
      ipv6 {
        address = "${local.service_mapping.proxy.ipv6}/64"
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
    mac_address = "bc:24:11:1d:2c:0f"
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
  }

  tags = ["legacyv4", "lxc", "traefik"]

  console {
    enabled   = true
    tty_count = 2
    type      = "tty"
  }

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

# BGP failover LXC on vault. eth0 rides the mbrains OOB bridge with DHCP;
# eth1 holds the static transit addresses on mwanbr.
resource "proxmox_virtual_environment_container" "mwan_failover" {
  node_name = "vault"
  vm_id     = local.service_mapping.mwan_failover.vmid

  initialization {
    hostname = "mwan-failover"
    ip_config {
      ipv4 {
        address = "dhcp"
      }
    }
    ip_config {
      ipv4 {
        address = "10.250.250.4/29"
      }
      ipv6 {
        address = "${local.service_mapping.mwan_failover.ipv6}/64"
      }
    }
  }

  features {
    nesting = true
  }

  network_interface {
    name        = "eth0"
    bridge      = "mbrains"
    mac_address = "BC:24:11:A1:06:E9"
  }

  network_interface {
    name        = "eth1"
    bridge      = "mwanbr"
    mac_address = "BC:24:11:E1:5C:F4"
  }

  disk {
    datastore_id = "local-lvm"
    size         = 4
  }

  memory {
    dedicated = 512
    swap      = 0
  }

  cpu {
    architecture = "amd64"
    cores        = 2
  }

  console {
    enabled   = true
    tty_count = 2
    type      = "tty"
  }

  operating_system {
    template_file_id = "storage:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started       = true
  start_on_boot = true
  unprivileged  = false

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      operating_system[0].template_file_id,
    ]
  }
}
