# Suburban testbed VMs managed by OpenTofu.
#
# The live `args` fields on the MWAN VM and the OPNsense VM are owned by Ansible because
# the Proxmox API rejects API-token writes to that field. The bpg/proxmox provider
# leaves undeclared fields alone, so live `args` drift does not surface in plan.
# The MWAN VM's args also carry its vsock CID, which tracks its vm_id.

# Resource name follows the canonical suburban mapping key.
resource "proxmox_virtual_environment_vm" "mwan_suburban" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.mwan_suburban.vmid
  name      = local.service_mapping.mwan_suburban.hostname

  depends_on = [
    proxmox_network_linux_bridge.vm_management_suburban,
    proxmox_network_linux_bridge.mwan_suburban,
    proxmox_network_linux_bridge.isp_webpass_suburban,
    proxmox_network_linux_bridge.isp_att_suburban,
    proxmox_network_linux_bridge.isp_mbrains_suburban,
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

  # MWAN-140 parity: the suburban MWAN management interface lives on the vmbrtrunk
  # services LAN, the same untagged segment as the suburban OPNsense LAN and DNS64
  # LXC, mirroring prod where the mwan VM enmgmt0 shares the
  # OPNsense LAN /64 and reaches the resolver on-link.
  network_device {
    bridge      = proxmox_network_linux_bridge.trunk_suburban.name
    model       = "virtio"
    mac_address = "BC:24:11:B3:9E:46"
  }

  network_device {
    bridge      = proxmox_network_linux_bridge.mwan_suburban.name
    model       = "virtio"
    mac_address = "BC:24:11:49:5D:94"
  }

  network_device {
    bridge      = proxmox_network_linux_bridge.isp_webpass_suburban.name
    model       = "virtio"
    mac_address = "BC:24:11:BE:8E:B4"
  }

  network_device {
    bridge      = proxmox_network_linux_bridge.isp_att_suburban.name
    model       = "virtio"
    mac_address = "BC:24:11:C0:D7:60"
  }

  network_device {
    bridge      = proxmox_network_linux_bridge.isp_mbrains_suburban.name
    model       = "virtio"
    mac_address = "BC:24:11:3D:CE:CC"
  }

  # No initialization block: the VM carries no cloud-init drive or values.
  # The guest configures its addresses statically via systemd-networkd, and
  # inventory reads them from service_mapping (MWAN-204).

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      # Ansible owns the live `args` field (vhost-vsock-pci); the Proxmox API
      # rejects token writes to it, so tofu must not try to change or null it.
      kvm_arguments,
    ]
  }
}

resource "proxmox_virtual_environment_vm" "opnsense_suburban" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.opnsense_suburban.vmid
  name      = local.service_mapping.opnsense_suburban.hostname

  depends_on = [
    proxmox_network_linux_bridge.mwan_suburban,
    proxmox_network_linux_bridge.trunk_suburban,
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
    bridge      = proxmox_network_linux_bridge.trunk_suburban.name
    model       = "virtio"
    mac_address = "BC:24:11:7D:6D:87"
  }

  network_device {
    bridge      = proxmox_network_linux_bridge.mwan_suburban.name
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
