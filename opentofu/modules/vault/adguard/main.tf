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

variable "service_mapping" { type = any }

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
