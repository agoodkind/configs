# Developer sandbox LXC on vault. Ansible does not manage its inside.
resource "proxmox_virtual_environment_container" "debianct" {
  node_name = "vault"
  vm_id     = local.service_mapping.debianct.vmid

  initialization {
    hostname = split(".", local.service_mapping.debianct.hostname)[0]
    dns {
      domain = regex("^[^.]+\\.(.+)$", local.service_mapping.debianct.hostname)[0]
    }
    ip_config {
      ipv6 {
        address = "${local.service_mapping.debianct.ipv6}/64"
        gateway = local.service_mapping.vault_hypervisor.ipv6
      }
    }
  }

  features {
    nesting = true
    fuse    = true
  }

  network_interface {
    name        = "eth0"
    bridge      = "vmnet"
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

variable "service_mapping" { type = object({
  debianct = object({
    vmid     = number
    hostname = string
    ipv6     = string
  })
  vault_hypervisor = object({
    ipv6 = string
  })
}) }

variable "ssh_keys" { type = string }

locals { service_mapping = var.service_mapping }

terraform {
  required_providers {
    proxmox = {
      source  = "bpg/proxmox"
      version = ">= 0.106.0"
    }
  }
}
