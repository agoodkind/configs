# Suburban testbed VMs managed by OpenTofu.
#
# The live `args` fields on VM 950 and VM 101 are owned by Ansible because the
# Proxmox API rejects API-token writes to that field. The bpg/proxmox provider
# leaves undeclared fields alone, so live `args` drift does not surface in plan.

resource "proxmox_virtual_environment_vm" "vm950_test_mwan" {
  node_name = "hypervisor"
  vm_id     = 950
  name      = "test-mwan"

  depends_on = [
    proxmox_network_linux_bridge.vm_management,
    proxmox_network_linux_bridge.mwan_internal,
    proxmox_network_linux_bridge.isp_webpass,
    proxmox_network_linux_bridge.isp_att,
    proxmox_network_linux_bridge.isp_mbrains,
  ]

  machine       = "q35"
  scsi_hardware = "virtio-scsi-pci"
  bios          = "seabios"
  on_boot       = false
  started       = true

  keyboard_layout = "en-us"

  agent {
    enabled = true
    type    = "virtio"
  }

  cpu {
    cores = 2
  }

  memory {
    dedicated = 2048
  }

  operating_system {
    type = "l26"
  }

  serial_device {
    device = "socket"
  }

  vga {
    type = "serial0"
  }

  disk {
    datastore_id = "local-zfs"
    interface    = "scsi0"
    file_format  = "raw"
    size         = 16
    discard      = "on"
  }

  # MWAN-140 parity: VM 950 management lives on the vmbrtrunk 204:: services
  # LAN, the same untagged segment as the testbed OPNsense LAN (204::1) and the
  # DNS64 LXC (204::464), mirroring prod where the mwan VM enmgmt0 shares the
  # OPNsense LAN /64 and reaches the resolver on-link.
  network_device {
    bridge      = "vmbrtrunk"
    model       = "virtio"
    mac_address = "BC:24:11:B3:9E:46"
  }

  network_device {
    bridge      = "vmbr2"
    model       = "virtio"
    mac_address = "BC:24:11:49:5D:94"
  }

  network_device {
    bridge      = "vmbr4"
    model       = "virtio"
    mac_address = "BC:24:11:BE:8E:B4"
  }

  network_device {
    bridge      = "vmbr5"
    model       = "virtio"
    mac_address = "BC:24:11:C0:D7:60"
  }

  network_device {
    bridge      = "vmbr6"
    model       = "virtio"
    mac_address = "BC:24:11:3D:CE:CC"
  }

  initialization {
    datastore_id = "local-lvm"

    ip_config {
      ipv4 {
        address = "dhcp"
      }
      ipv6 {
        address = "3d06:bad:b01:204::950/64"
        gateway = "3d06:bad:b01:204::1"
      }
    }

    user_account {
      username = "root"
      keys     = [var.ssh_keys]
    }
  }

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      initialization[0].user_account[0].keys,
      # Ansible owns the live `args` field (vhost-vsock-pci); the Proxmox API
      # rejects token writes to it, so tofu must not try to change or null it.
      kvm_arguments,
    ]
  }
}

resource "proxmox_virtual_environment_vm" "opnsense_test" {
  node_name = "hypervisor"
  vm_id     = 101
  name      = "opnsense-test"

  depends_on = [
    proxmox_network_linux_bridge.mwan_internal,
    proxmox_network_linux_bridge.trunk,
  ]

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

  disk {
    datastore_id = "local-zfs"
    interface    = "scsi0"
    size         = 16
  }

  network_device {
    bridge      = "vmbrtrunk"
    model       = "virtio"
    mac_address = "BC:24:11:7D:6D:87"
  }

  network_device {
    bridge      = "vmbr2"
    model       = "virtio"
    mac_address = "BC:24:11:0F:66:FA"
  }

  lifecycle {
    prevent_destroy = true
  }
}
