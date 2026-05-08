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
  cpu { cores = 2 }

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

# MWAN-62: suburban testbed LXCs.
#
# These resources codify the pre-existing suburban testbed containers that
# mwan-140/mwan-143 created via `pct create` and Ansible. Live discovery on
# 2026-05-07 (`pct config <id>` on suburban) was the source of truth for
# each resource. Operators import the running containers into Tofu state
# per opentofu/imports.md; the resource shape is set so `tofu plan` after
# import should report no changes (or only cosmetic drift on
# initialization fields, which the lifecycle blocks can ignore).
#
# Notes that may surface as drift on `tofu plan`:
#
# 1. None of these LXCs run cloud-init. The IP addresses on the live host
#    come from Proxmox-native `pct` config fields (visible as `ip=` and
#    `ip6=` on each net line). The bpg provider models the same field via
#    `initialization.ip_config`, so drift on that block typically means
#    the value here is wrong; tune the value rather than ignoring it.
#
# 2. The OS template field (`template_file_id`) is informational on
#    imported resources because Proxmox does not store the original
#    template name in `pct config`. The value is set to the Debian 13
#    standard template available on suburban's `local` storage so that
#    `tofu plan` has a stable reference; live drift on this field is
#    expected and ignored via lifecycle.
#
# 3. LXC 100 (mwan-failover-test) has a `parent: pre-vmbr6-move` snapshot
#    in the live config. Snapshots are not modeled by the bpg provider.
#    The live active config uses vmbr6 on net0 and vmbr2 on net1, which
#    differs from the [ansible-deploy-golden] snapshot. The active shape
#    is what this resource declares.

# Suburban LXC 100: mwan-failover-test container.
# Used as the failover target client for keepalived/VRRP testbed runs.
resource "proxmox_virtual_environment_container" "mwan_failover_test" {
  provider  = proxmox.suburban
  node_name = "hypervisor"
  vm_id     = 100

  initialization {
    hostname = "mwan-failover-test"
    ip_config {
      ipv4 {
        address = "dhcp"
      }
      ipv6 {
        address = "dhcp"
      }
    }
    ip_config {
      ipv4 {
        address = "10.250.250.4/29"
      }
      ipv6 {
        address = "3d06:bad:b01:201::4/64"
      }
    }
  }

  features {
    nesting = true
  }

  network_interface {
    name        = "eth0"
    bridge      = "vmbr6"
    mac_address = "BC:24:11:E7:86:B4"
  }

  network_interface {
    name        = "eth1"
    bridge      = "vmbr2"
    mac_address = "BC:24:11:00:97:29"
  }

  disk {
    datastore_id = "local-zfs"
    size         = 4
  }

  memory { dedicated = 512 }
  cpu { cores = 1 }

  tags = ["lxc", "mwan", "testbed"]

  operating_system {
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started      = true
  unprivileged = true

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      operating_system[0].template_file_id,
    ]
  }
}

# Suburban LXC 200: isp-webpass simulator.
# Provides a fake Webpass-style upstream on vmbr4 plus a vmbr0 management
# NIC for outbound updates and SSH.
resource "proxmox_virtual_environment_container" "isp_webpass" {
  provider  = proxmox.suburban
  node_name = "hypervisor"
  vm_id     = 200

  initialization {
    hostname = "isp-webpass"
    ip_config {
      ipv4 {
        address = "10.240.204.1/24"
      }
    }
    ip_config {
      ipv4 {
        address = "dhcp"
      }
    }
  }

  features {
    nesting = true
  }

  network_interface {
    name        = "eth0"
    bridge      = "vmbr4"
    mac_address = "BC:24:11:7F:DE:4E"
  }

  network_interface {
    name        = "eth1"
    bridge      = "vmbr0"
    mac_address = "BC:24:11:FC:17:A7"
  }

  disk {
    datastore_id = "local-zfs"
    size         = 2
  }

  memory { dedicated = 128 }
  cpu { cores = 1 }

  tags = ["lxc", "mwan", "testbed", "isp-sim"]

  operating_system {
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started      = true
  unprivileged = true

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      operating_system[0].template_file_id,
    ]
  }
}

# Suburban LXC 201: isp-att simulator.
# Provides a fake AT&T-style upstream on vmbr5 plus a vmbr0 management NIC.
resource "proxmox_virtual_environment_container" "isp_att" {
  provider  = proxmox.suburban
  node_name = "hypervisor"
  vm_id     = 201

  initialization {
    hostname = "isp-att"
    ip_config {
      ipv4 {
        address = "10.240.205.1/24"
      }
    }
    ip_config {
      ipv4 {
        address = "dhcp"
      }
    }
  }

  features {
    nesting = true
  }

  network_interface {
    name        = "eth0"
    bridge      = "vmbr5"
    mac_address = "BC:24:11:D4:3C:A4"
  }

  network_interface {
    name        = "eth1"
    bridge      = "vmbr0"
    mac_address = "BC:24:11:6C:B8:2B"
  }

  disk {
    datastore_id = "local-zfs"
    size         = 2
  }

  memory { dedicated = 128 }
  cpu { cores = 1 }

  tags = ["lxc", "mwan", "testbed", "isp-sim"]

  operating_system {
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started      = true
  unprivileged = true

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      operating_system[0].template_file_id,
    ]
  }
}

# Suburban LXC 202: isp-mbrains simulator.
# Provides a fake Monkeybrains-style upstream on vmbr6 with both IPv4 and
# IPv6, plus a vmbr0 management NIC.
resource "proxmox_virtual_environment_container" "isp_mbrains" {
  provider  = proxmox.suburban
  node_name = "hypervisor"
  vm_id     = 202

  initialization {
    hostname = "isp-mbrains"
    ip_config {
      ipv4 {
        address = "10.240.206.1/24"
      }
      ipv6 {
        address = "3d06:bad:b01:250::1/64"
      }
    }
    ip_config {
      ipv4 {
        address = "dhcp"
      }
    }
  }

  features {
    nesting = true
  }

  network_interface {
    name        = "eth0"
    bridge      = "vmbr6"
    mac_address = "BC:24:11:87:1F:3A"
  }

  network_interface {
    name        = "eth1"
    bridge      = "vmbr0"
    mac_address = "BC:24:11:DF:62:D3"
  }

  disk {
    datastore_id = "local-zfs"
    size         = 2
  }

  memory { dedicated = 128 }
  cpu { cores = 1 }

  tags = ["lxc", "mwan", "testbed", "isp-sim"]

  operating_system {
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started      = true
  unprivileged = true

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      operating_system[0].template_file_id,
    ]
  }
}

# Suburban LXC 203: testbed-proxy.
# Single-NIC LAN-side container on vmbr3 used as the OPNsense LAN client
# during cutover2 testing. Live config has a static IPv4 gateway
# (192.168.1.1) and IPv6 gateway (3d06:bad:b01:211::1).
resource "proxmox_virtual_environment_container" "testbed_proxy" {
  provider  = proxmox.suburban
  node_name = "hypervisor"
  vm_id     = 203

  initialization {
    hostname = "testbed-proxy"
    ip_config {
      ipv4 {
        address = "192.168.1.10/24"
        gateway = "192.168.1.1"
      }
      ipv6 {
        address = "3d06:bad:b01:211::10/64"
        gateway = "3d06:bad:b01:211::1"
      }
    }
  }

  features {
    nesting = true
  }

  network_interface {
    name        = "eth0"
    bridge      = "vmbr3"
    mac_address = "BC:24:11:4B:CC:BD"
  }

  disk {
    datastore_id = "local-zfs"
    size         = 2
  }

  memory { dedicated = 256 }
  cpu { cores = 1 }

  tags = ["lxc", "mwan", "testbed", "lan-client"]

  operating_system {
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started      = true
  unprivileged = true

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      operating_system[0].template_file_id,
    ]
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

  memory { dedicated = 8192 }
  cpu { cores = 2 }

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
