# Production three-node data tier and second app instance. These guests are
# declared ahead of their apply, which waits on an operator decision about
# hypervisor memory.

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
    datastore_id = "local-lvm"
    size         = 60
  }

  memory {
    dedicated = 16384
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
    datastore_id = "local-lvm"
    size         = 60
  }

  memory {
    dedicated = 16384
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
    datastore_id = "local-lvm"
    size         = 60
  }

  memory {
    dedicated = 16384
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
    datastore_id = "local-lvm"
    size         = 40
  }

  memory {
    dedicated = 8192
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
