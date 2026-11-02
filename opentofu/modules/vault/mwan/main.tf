# The LXC uses the host gateway on the private guest network.
resource "proxmox_virtual_environment_container" "mwan_failover" {
  node_name = "vault"
  vm_id     = local.service_mapping.mwan_failover.vmid

  initialization {
    hostname = "mwan-failover"
    dns {
      servers = [local.service_mapping.dns64.ipv6]
    }
    ip_config {
      ipv6 {
        address = format("%s/64", local.service_mapping.mwan_failover.ipv6)
        gateway = local.service_mapping.vault_hypervisor.ipv6
      }
    }
  }

  features {
    nesting = true
  }

  network_interface {
    name        = "eth0"
    bridge      = "vmbr0"
    mac_address = "BC:24:11:A1:06:E9"
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

variable "service_mapping" { type = any }
locals { service_mapping = var.service_mapping }

terraform {
  required_providers {
    proxmox = {
      source  = "bpg/proxmox"
      version = ">= 0.106.0"
    }
  }
}
