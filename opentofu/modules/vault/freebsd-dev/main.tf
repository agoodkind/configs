# Ad hoc FreeBSD development VM. Its disk was sized in megabytes at creation,
# which the provider cannot express, so the disk stays unmanaged.
resource "proxmox_virtual_environment_vm" "freebsd_dev" {
  node_name = "vault"
  vm_id     = local.service_mapping.freebsd_dev_home.vmid
  name      = local.service_mapping.freebsd_dev_home.hostname

  machine         = "q35"
  bios            = "ovmf"
  scsi_hardware   = "virtio-scsi-pci"
  on_boot         = false
  started         = false
  keyboard_layout = "en-us"

  agent {
    enabled = true
  }

  cpu {
    cores = 1
  }

  memory {
    dedicated = 4096
  }

  efi_disk {
    datastore_id = "local-lvm"
    type         = "4m"
  }

  serial_device {
    device = "socket"
  }

  network_device {
    bridge      = "vmbr0"
    model       = "virtio"
    mac_address = "BC:24:11:6C:74:7A"
  }

  initialization {
    datastore_id = "local-lvm"

    ip_config {
      ipv6 {
        address = "${local.service_mapping.freebsd_dev_home.ipv6}/64"
        gateway = "3d06:bad:b01::1"
      }
    }

    user_account {
      username = "agoodkind"
    }
  }

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      disk,
      initialization[0].user_account,
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
