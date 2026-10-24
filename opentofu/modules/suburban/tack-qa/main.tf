resource "proxmox_virtual_environment_container" "tack_app2_suburban" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.tack_app2_suburban.vmid


  initialization {
    hostname = local.service_mapping.tack_app2_suburban.hostname
    ip_config {
      ipv6 {
        address = "${local.service_mapping.tack_app2_suburban.ipv6}/64"
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
    bridge      = var.host_networks.trunk_suburban
    mac_address = local.service_mapping.tack_app2_suburban.mac_address
  }

  disk {
    datastore_id  = "local-zfs"
    size          = 30
    mount_options = ["discard"]
  }

  memory {
    dedicated = 2048
  }

  cpu {
    cores = 2
  }

  tags = ["lxc", "tack", "tack-app", "qa", "docker"]

  operating_system {
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started      = true
  unprivileged = true

  lifecycle {
    prevent_destroy = false
    ignore_changes = [
      # Proxmox does not return injected SSH keys, so a re-import would read
      # the configured keys as an addition that forces replacement.
      initialization[0].user_account,
      operating_system[0].template_file_id,
    ]
  }
}

resource "proxmox_virtual_environment_container" "tack_data1_suburban" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.tack_data1_suburban.vmid


  initialization {
    hostname = local.service_mapping.tack_data1_suburban.hostname
    ip_config {
      ipv6 {
        address = "${local.service_mapping.tack_data1_suburban.ipv6}/64"
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
    bridge      = var.host_networks.trunk_suburban
    mac_address = local.service_mapping.tack_data1_suburban.mac_address
  }

  disk {
    datastore_id  = "local-zfs"
    size          = 40
    mount_options = ["discard"]
  }

  memory {
    dedicated = 4096
  }

  cpu {
    cores = 2
  }

  tags = ["lxc", "tack", "tack-data", "qa", "docker"]

  operating_system {
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started      = true
  unprivileged = true

  lifecycle {
    prevent_destroy = false
    ignore_changes = [
      # Proxmox does not return injected SSH keys, so a re-import would read
      # the configured keys as an addition that forces replacement.
      initialization[0].user_account,
      operating_system[0].template_file_id,
    ]
  }
}

resource "proxmox_virtual_environment_container" "tack_data2_suburban" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.tack_data2_suburban.vmid


  initialization {
    hostname = local.service_mapping.tack_data2_suburban.hostname
    ip_config {
      ipv6 {
        address = "${local.service_mapping.tack_data2_suburban.ipv6}/64"
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
    bridge      = var.host_networks.trunk_suburban
    mac_address = local.service_mapping.tack_data2_suburban.mac_address
  }

  disk {
    datastore_id  = "local-zfs"
    size          = 40
    mount_options = ["discard"]
  }

  memory {
    dedicated = 4096
  }

  cpu {
    cores = 2
  }

  tags = ["lxc", "tack", "tack-data", "qa", "docker"]

  operating_system {
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started      = true
  unprivileged = true

  lifecycle {
    prevent_destroy = false
    ignore_changes = [
      # Proxmox does not return injected SSH keys, so a re-import would read
      # the configured keys as an addition that forces replacement.
      initialization[0].user_account,
      operating_system[0].template_file_id,
    ]
  }
}

resource "proxmox_virtual_environment_container" "tack_data3_suburban" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.tack_data3_suburban.vmid


  initialization {
    hostname = local.service_mapping.tack_data3_suburban.hostname
    ip_config {
      ipv6 {
        address = "${local.service_mapping.tack_data3_suburban.ipv6}/64"
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
    bridge      = var.host_networks.trunk_suburban
    mac_address = local.service_mapping.tack_data3_suburban.mac_address
  }

  disk {
    datastore_id  = "local-zfs"
    size          = 40
    mount_options = ["discard"]
  }

  memory {
    dedicated = 4096
  }

  cpu {
    cores = 2
  }

  tags = ["lxc", "tack", "tack-data", "qa", "docker"]

  operating_system {
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started      = true
  unprivileged = true

  lifecycle {
    prevent_destroy = false
    ignore_changes = [
      # Proxmox does not return injected SSH keys, so a re-import would read
      # the configured keys as an addition that forces replacement.
      initialization[0].user_account,
      operating_system[0].template_file_id,
    ]
  }
}

resource "proxmox_virtual_environment_container" "tack_qa_suburban" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.tack_qa_suburban.vmid


  initialization {
    hostname = local.service_mapping.tack_qa_suburban.hostname
    ip_config {
      ipv6 {
        address = "${local.service_mapping.tack_qa_suburban.ipv6}/64"
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
    bridge      = var.host_networks.trunk_suburban
    mac_address = "BC:24:11:04:00:00"
  }

  disk {
    datastore_id  = "local-zfs"
    size          = 40
    mount_options = ["discard"]
  }

  memory {
    dedicated = 8192
  }

  cpu {
    cores = 2
  }

  tags = ["lxc", "tack", "qa", "docker"]

  operating_system {
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started      = true
  unprivileged = true

  lifecycle {
    prevent_destroy = false
    ignore_changes = [
      # Proxmox does not return injected SSH keys, so a re-import would read
      # the configured keys as an addition that forces replacement.
      initialization[0].user_account,
      operating_system[0].template_file_id,
      # The backup root's volume on the slow storage tier (TACK-495). The
      # provider replaces a container to change its mount points, so the
      # volume is hot plugged with pct set and OpenTofu leaves it alone.
      mount_point,
    ]
  }
}

resource "proxmox_virtual_environment_container" "tack_search1_suburban" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.tack_search1_suburban.vmid


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
    bridge      = var.host_networks.trunk_suburban
    mac_address = local.service_mapping.tack_search1_suburban.mac_address
  }

  disk {
    datastore_id  = "local-zfs"
    size          = local.tack_search.tack_search_guest_disk_gib
    mount_options = ["discard"]
  }

  memory {
    dedicated = local.tack_search.tack_search_guest_memory_mib
  }

  cpu {
    cores = local.tack_search.tack_search_guest_cores
  }

  tags = ["lxc", "tack", "tack-search", "qa", "docker"]

  operating_system {
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started      = true
  unprivileged = true

  lifecycle {
    prevent_destroy = false
    ignore_changes = [
      # Proxmox does not return injected SSH keys. A re-import reads the
      # configured keys as an addition, and an addition forces replacement.
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

output "tack_qa_suburban_vmid" {
  description = "VMID assigned to the suburban Tack QA LXC."
  value       = proxmox_virtual_environment_container.tack_qa_suburban.vm_id
}

output "tack_qa_suburban_ipv6" {
  description = "IPv6 address assigned to the suburban Tack QA LXC."
  value       = local.service_mapping.tack_qa_suburban.ipv6
}
