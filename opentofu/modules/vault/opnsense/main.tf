# This module manages the virtual hardware and boot settings of the
# production OPNsense router on vault.
resource "proxmox_virtual_environment_vm" "opnsense" {
  node_name = "vault"
  vm_id     = local.service_mapping.opnsense.vmid
  name      = "router.home.goodkind.io"
  tags      = ["legacyv4", "noramusage"]

  # The provider recalculates guest agent addresses and interface names when
  # power state or network devices change. The provider can write VM settings
  # during an apply even when the plan lists only computed output changes.
  # Compare the live VM configuration before and after applying a plan.
  machine         = "q35"
  bios            = "ovmf"
  scsi_hardware   = "virtio-scsi-single"
  on_boot         = true
  started         = true
  keyboard_layout = "en-us"

  agent {
    enabled = true
  }

  cpu {
    type  = "host"
    cores = 4
  }

  memory {
    dedicated = 8192
  }

  operating_system {
    type = "other"
  }

  efi_disk {
    datastore_id      = "local-lvm"
    type              = "4m"
    pre_enrolled_keys = true
  }

  disk {
    datastore_id = "local-lvm"
    interface    = "scsi0"
    size         = 60
    cache        = "writeback"
    discard      = "on"
    iothread     = true
    ssd          = true
  }

  hostpci {
    device = "hostpci0"
    id     = "0000:02:0a"
    rombar = true
  }

  serial_device {
    device = "socket"
  }

  # The provider assigns consecutive Proxmox slots from this list. FreeBSD
  # numbers virtio interfaces in slot order at boot. Reordering adapters
  # changes their interface names without updating the saved OPNsense
  # interface assignments.
  network_device {
    bridge      = "vmbr0"
    model       = "virtio"
    mac_address = "BC:24:11:AD:E8:92"
  }

  network_device {
    bridge      = "mwanbr"
    model       = "virtio"
    mac_address = "BC:24:11:99:20:E4"
  }

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
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
