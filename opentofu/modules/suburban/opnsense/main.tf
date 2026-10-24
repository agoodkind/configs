resource "proxmox_virtual_environment_vm" "opnsense_suburban" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.opnsense_suburban.vmid
  name      = local.service_mapping.opnsense_suburban.hostname


  scsi_hardware   = "virtio-scsi-pci"
  on_boot         = true
  started         = true
  keyboard_layout = "en-us"

  agent {
    enabled = true
    type    = "virtio"
  }

  cpu {
    cores = 2
  }

  memory {
    dedicated = 4096
  }

  serial_device {
    device = "socket"
  }

  vga {
    type = "serial0"
  }

  # Sized to match the live VM, which was grown in place to hold the firmware
  # upgrades and the snapshot chain. Proxmox cannot shrink a disk, so understating
  # this makes a plan propose a shrink.
  disk {
    datastore_id = "local-zfs"
    interface    = "scsi0"
    size         = 40
  }

  network_device {
    bridge      = var.host_networks.trunk_suburban
    model       = "virtio"
    mac_address = "BC:24:11:7D:6D:87"
  }

  network_device {
    bridge      = var.host_networks.mwan_suburban
    model       = "virtio"
    mac_address = "BC:24:11:0F:66:FA"
  }

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      # Ansible owns the live `args` field, which carries the virtio-serial
      # chardev the mwan-opnsense out-of-band daemon serves on. Undeclared here,
      # tofu reads it as removed and an apply would null the break-glass channel.
      kvm_arguments,
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

output "opnsense_suburban_vmid" {
  description = "VMID assigned to the suburban OPNsense VM."
  value       = proxmox_virtual_environment_vm.opnsense_suburban.vm_id
}
