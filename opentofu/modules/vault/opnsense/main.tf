# The OPNsense LAN router. Its NICs live at net0 and net3 on the hypervisor,
# and the provider can only model a sequential list, so the NIC layout stays
# unmanaged; renumbering it live would rename the router's FreeBSD interfaces.
resource "proxmox_virtual_environment_vm" "opnsense" {
  node_name = "vault"
  vm_id     = local.service_mapping.opnsense.vmid
  name      = "router.home.goodkind.io"
  tags      = ["legacyv4", "noramusage"]

  # The provider may recalculate guest-agent addresses and interface names
  # after detecting a power or NIC diff. A computed-only plan does not restart
  # this VM; check for configured field changes before applying one.
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

  network_device {
    bridge      = "vmbr0"
    model       = "virtio"
    mac_address = "BC:24:11:AD:E8:92"
  }

  network_device {
    bridge      = "mwanbr"
    model       = "virtio"
    mac_address = "BC:24:11:77:50:0C"
  }

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      kvm_arguments,
      network_device,
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
