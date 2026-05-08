# MWAN-62 (partial): VM 950 (test-mwan) on suburban managed by OpenTofu.
#
# This resource codifies the suburban MWAN testbed VM that mwan-143 created
# via a shell script. It mirrors prod VM 113 with five NICs (mgmt, internal,
# three simulated ISP WANs) and ships the vhost-vsock-pci device so the
# watchdog on suburban talks to the in-VM mwan-agent over native vsock
# instead of the noisy TCP fallback.
#
# Live discovery on 2026-05-07 (qm config 950) was the source of truth for
# this resource. Operators import the running VM into Tofu state per
# opentofu/imports.md; the resource shape is set so `tofu plan` after import
# should report no changes (or only cosmetic comment-style diffs).
#
# Out of scope for this slice: suburban LXCs (200, 201, 202, 203) and
# suburban OPNsense VM 101. Those land in a follow-up MWAN-62 slice.
#
# Schema reference (bpg/proxmox >= 0.70):
#   https://registry.terraform.io/providers/bpg/proxmox/latest/docs/resources/virtual_environment_vm
#
# Assumption flagged: bpg/proxmox documents `kvm_arguments` as the field
# carrying raw QEMU arguments. The Proxmox API field is `args`; the provider
# maps `kvm_arguments` to it. If `tofu plan` reports drift on this field,
# verify the provider mapping against the upstream issue tracker before
# changing the value.

resource "proxmox_virtual_environment_vm" "vm950_test_mwan" {
  provider  = proxmox.suburban
  node_name = "hypervisor"
  vm_id     = 950
  name      = "test-mwan"

  machine       = "q35"
  scsi_hardware = "virtio-scsi-pci"
  bios          = "seabios"
  on_boot       = false
  started       = true

  # Raw QEMU arg adding the vsock device so mwan-watchdog-testbed can use
  # native vsock to reach the in-VM mwan-agent. Mirrors prod VM 113.
  kvm_arguments = "-device vhost-vsock-pci,guest-cid=950"

  agent {
    enabled = true
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

  # Boot disk imported from the Debian 13 generic cloud image already
  # staged at /var/lib/vz/template/iso/debian-13-generic-amd64.qcow2 on
  # suburban. After import the disk is raw format on local-zfs.
  disk {
    datastore_id = "local-zfs"
    interface    = "scsi0"
    file_format  = "raw"
    size         = 16
    discard      = "on"
  }

  # Five NICs in the same order qm reported them. MAC addresses come from
  # live qm config so the testbed-mwan_testbed_servers.yml MAC pins keep
  # working without churn.

  # net0: management (vmbr1, prod-equivalent of MGMT plane).
  network_device {
    bridge      = "vmbr1"
    model       = "virtio"
    mac_address = "BC:24:11:B3:9E:46"
  }

  # net1: internal link to OPNsense (vmbr2).
  network_device {
    bridge      = "vmbr2"
    model       = "virtio"
    mac_address = "BC:24:11:49:5D:94"
  }

  # net2: simulated WAN webpass (vmbr4).
  network_device {
    bridge      = "vmbr4"
    model       = "virtio"
    mac_address = "BC:24:11:BE:8E:B4"
  }

  # net3: simulated WAN AT&T (vmbr5).
  network_device {
    bridge      = "vmbr5"
    model       = "virtio"
    mac_address = "BC:24:11:C0:D7:60"
  }

  # net4: simulated WAN Monkeybrains (vmbr6).
  network_device {
    bridge      = "vmbr6"
    model       = "virtio"
    mac_address = "BC:24:11:3D:CE:CC"
  }

  initialization {
    datastore_id = "local-zfs"

    ip_config {
      ipv4 {
        address = "dhcp"
      }
      ipv6 {
        address = "3d06:bad:b01:200::950/64"
        gateway = "fe80::1"
      }
    }

    user_account {
      username = "root"
      keys     = [trimspace(data.http.github_ssh_keys.response_body)]
    }
  }

  lifecycle {
    prevent_destroy = true
    # The api_token field exposed in qm config (sshkeys URL-escape, vmgenid)
    # changes between provider versions. Ignore those so plan-noise is low
    # after import. Operators tune this list as drift surfaces.
    ignore_changes = [
      initialization[0].user_account[0].keys,
    ]
  }
}
