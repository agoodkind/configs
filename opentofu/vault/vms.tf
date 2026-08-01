# Production VMs on vault, imported from the live hypervisor. Sizing and
# addressing mirror the running guests; Ansible owns everything inside them.
#
# The live `args` fields (the router's virtio-serial chardev, the MWAN VM's
# vsock device) are owned by Ansible because the Proxmox API rejects API-token
# writes to that field, so kvm_arguments stays unmanaged everywhere it exists.

# The OPNsense LAN router. Its NICs live at net0 and net3 on the hypervisor,
# and the provider can only model a sequential list, so the NIC layout stays
# unmanaged; renumbering it live would rename the router's FreeBSD interfaces.
resource "proxmox_virtual_environment_vm" "opnsense" {
  node_name = "vault"
  vm_id     = local.service_mapping.opnsense.vmid
  name      = "router.home.goodkind.io"
  tags      = ["legacyv4", "noramusage"]

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

  initialization {
    datastore_id = "local-lvm"

    ip_config {
      ipv4 {
        address = "10.250.0.113/24"
        gateway = "10.250.0.1"
      }
      ipv6 {
        address = "${local.service_mapping.mwan.ipv6}/64"
        gateway = "3d06:bad:b01::1"
      }
    }

    dns {
      domain  = "home.goodkind.io"
      servers = ["3d06:bad:b01::6", "10.250.0.6"]
    }

    user_account {
      username = "root"
    }
  }

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      kvm_arguments,
      # The live VM carries cloud-init values without an attached cloud-init
      # drive; managing initialization would attach one on apply.
      initialization,
    ]
  }
}

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
