# BGP failover LXC on vault. eth0 rides the mbrains OOB bridge with DHCP;
# eth1 holds the static transit addresses on mwanbr.
resource "proxmox_virtual_environment_container" "mwan_failover" {
  node_name = "vault"
  vm_id     = local.service_mapping.mwan_failover.vmid

  initialization {
    hostname = "mwan-failover"
    ip_config {
      ipv4 {
        address = "dhcp"
      }
    }
    ip_config {
      ipv4 {
        address = "10.250.250.4/29"
      }
      ipv6 {
        address = "${local.service_mapping.mwan_failover.ipv6}/64"
      }
    }
  }

  features {
    nesting = true
  }

  network_interface {
    name        = "eth0"
    bridge      = "mbrains"
    mac_address = "BC:24:11:A1:06:E9"
  }

  network_interface {
    name        = "eth1"
    bridge      = "mwanbr"
    mac_address = "BC:24:11:E1:5C:F4"
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

resource "proxmox_virtual_environment_vm" "mwan" {
  node_name = "vault"
  vm_id     = local.service_mapping.mwan.vmid
  name      = "mwan.home.goodkind.io"
  tags      = ["legacyv4", "mwan", "network", "vm"]

  machine         = "q35"
  bios            = "ovmf"
  scsi_hardware   = "virtio-scsi-pci"
  on_boot         = true
  started         = true
  keyboard_layout = "en-us"

  agent {
    enabled = true
  }

  cpu {
    type  = "host"
    cores = 2
  }

  memory {
    dedicated = 4096
  }

  operating_system {
    type = "l26"
  }

  efi_disk {
    datastore_id      = "local-lvm"
    type              = "4m"
    pre_enrolled_keys = true
  }

  disk {
    datastore_id = "local-lvm"
    interface    = "scsi0"
    size         = 16
  }

  hostpci {
    device = "hostpci0"
    id     = "02:02.0"
    rombar = true
  }

  hostpci {
    device = "hostpci1"
    id     = "06:00.0"
    rombar = true
  }

  serial_device {
    device = "socket"
  }

  vga {
    type = "virtio"
  }

  network_device {
    bridge      = "vmbr0"
    model       = "virtio"
    mac_address = "BC:24:11:25:62:39"
  }

  network_device {
    bridge      = "mwanbr"
    model       = "virtio"
    mac_address = "BC:24:11:72:00:C1"
  }

  network_device {
    bridge      = "mbrains"
    model       = "virtio"
    mac_address = "BC:24:11:E6:58:31"
  }

  # No initialization block: the VM carries no cloud-init drive or values.
  # The guest configures its addresses statically via systemd-networkd, and
  # inventory reads them from service_mapping (MWAN-204).

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
