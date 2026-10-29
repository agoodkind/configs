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
