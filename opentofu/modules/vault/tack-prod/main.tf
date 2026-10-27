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
  # shrink that either fails or damages the store. discard returns each freed
  # block to the thin pool as it is freed, which the pool handles without
  # passing it to the drive (TACK-498); the guest reads it at its next start.
  disk {
    datastore_id  = "local-lvm"
    size          = 300
    mount_options = ["discard"]
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
      # The backup root's volume on the slow storage tier (TACK-495). The
      # provider replaces a container to change its mount points, so the
      # volume is hot plugged with pct set and OpenTofu leaves it alone.
      mount_point,
    ]
  }
}

resource "proxmox_virtual_environment_container" "tack_app2" {
  node_name = "vault"
  vm_id     = local.service_mapping.tack_app2.vmid

  initialization {
    hostname = local.service_mapping.tack_app2.hostname
    ip_config {
      ipv6 {
        address = "${local.service_mapping.tack_app2.ipv6}/64"
        gateway = local.service_mapping.opnsense.ipv6
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
    mac_address = local.service_mapping.tack_app2.mac_address
  }

  disk {
    datastore_id  = "local-lvm"
    size          = 40
    mount_options = ["discard"]
  }

  memory {
    dedicated = 4096
  }

  cpu {
    cores = 4
  }

  tags = ["lxc", "tack", "tack-app", "docker"]

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

resource "proxmox_virtual_environment_container" "tack_data1" {
  node_name = "vault"
  vm_id     = local.service_mapping.tack_data1.vmid

  initialization {
    hostname = local.service_mapping.tack_data1.hostname
    ip_config {
      ipv6 {
        address = "${local.service_mapping.tack_data1.ipv6}/64"
        gateway = local.service_mapping.opnsense.ipv6
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
    mac_address = local.service_mapping.tack_data1.mac_address
  }

  disk {
    datastore_id  = "local-lvm"
    size          = 60
    mount_options = ["discard"]
  }

  memory {
    dedicated = 12288
  }

  cpu {
    cores = 4
  }

  tags = ["lxc", "tack", "tack-data", "docker"]

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

resource "proxmox_virtual_environment_container" "tack_data2" {
  node_name = "vault"
  vm_id     = local.service_mapping.tack_data2.vmid

  initialization {
    hostname = local.service_mapping.tack_data2.hostname
    ip_config {
      ipv6 {
        address = "${local.service_mapping.tack_data2.ipv6}/64"
        gateway = local.service_mapping.opnsense.ipv6
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
    mac_address = local.service_mapping.tack_data2.mac_address
  }

  disk {
    datastore_id  = "local-lvm"
    size          = 60
    mount_options = ["discard"]
  }

  memory {
    dedicated = 12288
  }

  cpu {
    cores = 4
  }

  tags = ["lxc", "tack", "tack-data", "docker"]

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

resource "proxmox_virtual_environment_container" "tack_data3" {
  node_name = "vault"
  vm_id     = local.service_mapping.tack_data3.vmid

  initialization {
    hostname = local.service_mapping.tack_data3.hostname
    ip_config {
      ipv6 {
        address = "${local.service_mapping.tack_data3.ipv6}/64"
        gateway = local.service_mapping.opnsense.ipv6
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
    mac_address = local.service_mapping.tack_data3.mac_address
  }

  disk {
    datastore_id  = "local-lvm"
    size          = 60
    mount_options = ["discard"]
  }

  memory {
    dedicated = 12288
  }

  cpu {
    cores = 4
  }

  tags = ["lxc", "tack", "tack-data", "docker"]

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
    datastore_id  = "local-lvm"
    size          = local.tack_search.tack_search_guest_disk_gib
    mount_options = ["discard"]
  }

  memory {
    dedicated = local.tack_search.tack_search_guest_memory_mib
  }

  cpu {
    cores = local.tack_search.tack_search_guest_cores
  }

  tags = ["lxc", "tack", "tack-search", "docker"]

  operating_system {
    template_file_id = "storage:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started       = true
  start_on_boot = true
  unprivileged  = true

  lifecycle {
    prevent_destroy = true
    # Proxmox returns neither the injected SSH keys nor the template name.
    ignore_changes = [
      initialization[0].user_account,
      operating_system[0].template_file_id,
    ]
  }
}

variable "service_mapping" { type = any }

variable "ssh_keys" { type = string }

variable "host_networks" { type = map(string) }

locals { service_mapping = var.service_mapping }

variable "tack_search" { type = any }

locals { tack_search = var.tack_search }

terraform {
  required_providers {
    proxmox = {
      source  = "bpg/proxmox"
      version = ">= 0.106.0"
    }
  }
}

output "tack_vmid" {
  description = "VMID assigned to the Tack LXC container."
  value       = proxmox_virtual_environment_container.tack.vm_id
}

output "tack_ipv6" {
  description = "IPv6 address of the Tack LXC container."
  value       = "3d06:bad:b01::117"
}
