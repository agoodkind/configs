# Suburban testbed VMs managed by OpenTofu.
#
# The live `args` fields on the MWAN VM and VM 201 are owned by Ansible because
# the Proxmox API rejects API-token writes to that field. The bpg/proxmox provider
# leaves undeclared fields alone, so live `args` drift does not surface in plan.
# The MWAN VM's args also carry its vsock CID, which tracks its vm_id.

# Resource name deliberately omits the VMID so it cannot go stale again.
resource "proxmox_virtual_environment_vm" "test_mwan" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.test_mwan.vmid
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
    # local-lvm is disabled on suburban; the cloud-init drive lives on the same
    # active zfs pool as the disk.
    datastore_id = "local-zfs"

    ip_config {
      ipv4 {
        address = "dhcp"
      }
      ipv6 {
        address = "3d06:bad:b01:210::113/64"
        gateway = "3d06:bad:b01:210::1"
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
  vm_id     = local.service_mapping.opnsense_test.vmid
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

  # Sized to match the live VM, which was grown in place to hold the firmware
  # upgrades and the snapshot chain. Proxmox cannot shrink a disk, so understating
  # this makes a plan propose a shrink.
  disk {
    datastore_id = "local-zfs"
    interface    = "scsi0"
    size         = 40
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
    ignore_changes = [
      # Ansible owns the live `args` field, which carries the virtio-serial
      # chardev the mwan-opnsense out-of-band daemon serves on. Undeclared here,
      # tofu reads it as removed and an apply would null the break-glass channel.
      kvm_arguments,
    ]
  }
}
