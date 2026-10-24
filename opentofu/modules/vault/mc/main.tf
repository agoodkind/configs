# Minecraft LXC on vault (VMID 109).
resource "proxmox_virtual_environment_container" "mc" {
  node_name = "vault"
  vm_id     = local.service_mapping.mc.vmid

  initialization {
    hostname = "mc.home.goodkind.io"
    ip_config {
      ipv6 {
        address = "3d06:bad:b01::109/64"
        gateway = "3d06:bad:b01::1"
      }
    }
  }

  network_interface {
    name        = "eth0"
    bridge      = "vmbr0"
    mac_address = "BC:24:11:16:C7:47"
  }

  disk {
    datastore_id = "local-lvm"
    size         = 20
  }

  memory {
    dedicated = 8192
    swap      = 512
  }

  cpu {
    architecture = "amd64"
    cores        = 4
    limit        = 0
  }

  console {
    enabled   = true
    tty_count = 2
    type      = "tty"
  }

  tags = ["lxc", "mc", "minecraft"]

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
