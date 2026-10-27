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
