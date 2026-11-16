resource "proxmox_virtual_environment_container" "clyde_suburban" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.clyde_suburban.vmid


  initialization {
    hostname = local.service_mapping.clyde_suburban.hostname
    ip_config {
      ipv6 {
        address = "${local.service_mapping.clyde_suburban.ipv6}/64"
        gateway = local.service_mapping.opnsense_suburban.ipv6_vmnet
      }
    }
    dns {
      servers = ["2a07:a8c0::7d:698e", "2a07:a8c1::7d:698e"]
    }
    user_account {
      keys = [var.ssh_keys]
    }
  }

  network_interface {
    name        = "eth0"
    bridge      = var.host_networks.trunk_suburban
    mac_address = local.service_mapping.clyde_suburban.mac_address
  }

  disk {
    datastore_id = "local-zfs"
    size         = 4
  }

  memory {
    dedicated = 512
  }

  cpu {
    cores = 1
  }

  tags = ["lxc", "clyde", "testbed"]

  operating_system {
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started      = true
  unprivileged = true

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      initialization[0].user_account,
      operating_system[0].template_file_id,
    ]
  }
}

variable "service_mapping" { type = object({
  clyde_suburban = object({
    vmid        = number
    hostname    = string
    ipv6        = string
    mac_address = string
  })
  opnsense_suburban = object({
    ipv6_vmnet = string
  })
}) }

variable "ssh_keys" { type = string }

variable "host_networks" { type = map(string) }

locals { service_mapping = var.service_mapping }

terraform {
  required_providers {
    proxmox = {
      source  = "bpg/proxmox"
      version = ">= 0.106.0"

    }
  }
}
