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

# MWAN-62: suburban testbed OPNsense VM 101 (opnsense-test).
#
# This VM is the testbed counterpart of the prod OPNsense router. It boots
# from scsi0 on local-zfs (8G) and exposes the mwan-opnsense virtio-serial
# RPC channel via the `args` block. Mirrors the prod VM 101 shape on vault
# minus the LAN trunk plumbing (the trunk-mode iavf0 NIC mapping for
# vmbrtrunk lands in a follow-up slice tied to MWAN-140 slice 1).
#
# Live state caveat (handed off 2026-05-08): VM 101 is wedged from the
# MWAN-119 v2 rollback. The resource definition here documents the shape
# the live config reports right now (`qm config 101` on 2026-05-07) so
# `tofu import` succeeds; the wedged guest disk content is orthogonal to
# the resource shape.
#
# Discovered fields not modeled here:
#   * `unused0: local-zfs:vm-101-disk-0` is an orphan disk left from a
#     prior reinstall. The bpg provider does not model unused disks; the
#     operator either deletes it manually or leaves it in place. Drift is
#     not expected because Tofu only sees declared disks.
#   * `parent: mwan119-v2-preapply-20260508-0110` is a snapshot. Snapshots
#     are not modeled by the bpg provider.
#   * `smbios1: uuid=...` and `vmgenid: ...` are auto-generated. They
#     surface as drift on first plan and are normally ignored.

resource "proxmox_virtual_environment_vm" "opnsense_test" {
  provider  = proxmox.suburban
  node_name = "hypervisor"
  vm_id     = 101
  name      = "opnsense-test"

  scsi_hardware = "virtio-scsi-pci"
  on_boot       = false
  started       = true

  # Raw QEMU args block adds the virtio-serial-pci controller plus the
  # mwan-opnsense chardev that the in-host watchdog connects to. The
  # control socket lives at /var/run/qemu-server/101.mwanrpc; it is owned
  # by qemu-server and the lifecycle is tied to the VM. The chardev name
  # `io.goodkind.mwan-opnsense.0` is what the OPNsense plugin opens on
  # /dev/ttyV0.0 inside the guest.
  kvm_arguments = "-device virtio-serial-pci,id=mwanrpc -chardev socket,id=mwanchr,path=/var/run/qemu-server/101.mwanrpc,server=on,wait=off -device virtserialport,chardev=mwanchr,name=io.goodkind.mwan-opnsense.0"

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
    type = "other"
  }

  serial_device {
    device = "socket"
  }

  vga {
    type = "serial0"
  }

  # Boot disk. OPNsense was installed by the operator from ISO; the disk
  # currently holds the wedged config from the MWAN-119 v2 rollback. The
  # provider needs the disk shape to match what `qm config` reports.
  disk {
    datastore_id = "local-zfs"
    interface    = "scsi0"
    size         = 8
  }

  # net0: WAN side on vmbr3 (testbed-proxy LAN, used as OPNsense's
  # upstream during cutover2 dry runs).
  network_device {
    bridge      = "vmbr3"
    model       = "virtio"
    mac_address = "BC:24:11:5A:2E:A0"
  }

  # net1: LAN side on vmbr2 (mwan testbed internal segment).
  network_device {
    bridge      = "vmbr2"
    model       = "virtio"
    mac_address = "BC:24:11:EC:EF:CC"
  }

  lifecycle {
    prevent_destroy = true
    # smbios1.uuid and vmgenid auto-rotate on some Proxmox upgrades. They
    # surface as drift but never represent meaningful intent. The operator
    # tunes this list as drift surfaces.
    ignore_changes = [
      vga,
    ]
  }
}
