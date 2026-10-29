resource "proxmox_virtual_environment_container" "mwan_failover_suburban" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.mwan_failover_suburban.vmid


  initialization {
    hostname = local.service_mapping.mwan_failover_suburban.hostname
    ip_config {
      ipv4 {
        address = "dhcp"
      }
    }
    ip_config {
      ipv4 {
        address = "${local.service_mapping.mwan_failover_suburban.ipv4_transit}/29"
      }
      ipv6 {
        address = "${local.service_mapping.mwan_failover_suburban.ipv6_transit}/64"
      }
    }
    # Guest segment. Deploys target this address, so it carries no gateway:
    # the ISP-sim default route on eth0 is what this guest uses for egress,
    # and a second default would fight it.
    ip_config {
      ipv4 {
        address = "${local.service_mapping.mwan_failover_suburban.ipv4_vmnet}/24"
      }
      ipv6 {
        address = "${local.service_mapping.mwan_failover_suburban.ipv6_vmnet}/64"
      }
    }
    user_account {
      keys = [var.ssh_keys]
    }
  }

  # No features block. The failover runs systemd, the mwan binary, and
  # nftables, none of which need container features, and Proxmox refuses
  # feature-flag writes from non-root@pam actors on privileged containers.

  network_interface {
    name        = "eth0"
    bridge      = var.host_networks.isp_mbrains_suburban
    mac_address = "BC:24:11:E7:86:B4"
  }

  network_interface {
    name        = "eth1"
    bridge      = var.host_networks.mwan_suburban
    mac_address = "BC:24:11:00:97:29"
  }

  network_interface {
    name        = "eth2"
    bridge      = var.host_networks.trunk_suburban
    mac_address = "BC:24:11:04:21:60"
  }

  disk {
    datastore_id = "local-zfs"
    size         = 4
  }

  memory {
    dedicated = 512
    swap      = 512
  }

  cpu {
    architecture = "amd64"
    cores        = 1
    limit        = 0
  }

  console {
    enabled   = true
    tty_count = 2
    type      = "tty"
  }

  tags = []

  operating_system {
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started       = true
  start_on_boot = true
  unprivileged  = true

  lifecycle {
    prevent_destroy = false
    ignore_changes = [
      initialization[0].user_account,
      operating_system[0].template_file_id,
    ]
  }
}

# Resource name follows the canonical suburban mapping key.
resource "proxmox_virtual_environment_vm" "mwan_suburban" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.mwan_suburban.vmid
  name      = local.service_mapping.mwan_suburban.hostname


  machine       = "q35"
  scsi_hardware = "virtio-scsi-pci"
  bios          = "seabios"
  on_boot       = false
  started       = true

  # The provider does not restart the gateway for an update.
  reboot_after_update = false

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
    bridge      = var.host_networks.trunk_suburban
    model       = "virtio"
    mac_address = "BC:24:11:B3:9E:46"
  }

  network_device {
    bridge      = var.host_networks.mwan_suburban
    model       = "virtio"
    mac_address = "BC:24:11:49:5D:94"
  }

  network_device {
    bridge      = var.host_networks.isp_webpass_suburban
    model       = "virtio"
    mac_address = "BC:24:11:BE:8E:B4"
  }

  network_device {
    bridge      = var.host_networks.isp_att_suburban
    model       = "virtio"
    mac_address = "BC:24:11:C0:D7:60"
  }

  network_device {
    bridge      = var.host_networks.isp_mbrains_suburban
    model       = "virtio"
    mac_address = "BC:24:11:3D:CE:CC"
  }

  network_device {
    bridge      = var.host_networks.isp_astound_suburban
    model       = "virtio"
    mac_address = "BC:24:11:A5:70:06"
  }

  network_device {
    bridge      = var.host_networks.isp_routed_suburban
    model       = "virtio"
    mac_address = "BC:24:11:A5:70:08"
  }

  # Proxmox numbers these devices in key order after the seven devices above.
  # A new key must sort after the existing keys.
  dynamic "network_device" {
    for_each = local.service_mapping.mwan_suburban.routing_interfaces

    content {
      bridge      = local.routing_bridge_names[network_device.value.network]
      model       = "virtio"
      mac_address = network_device.value.mac_address
    }
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

locals { routing_bridge_names = var.host_networks }

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

output "mwan_suburban_vmid" {
  description = "VMID assigned to the suburban MWAN VM."
  value       = proxmox_virtual_environment_vm.mwan_suburban.vm_id
}
