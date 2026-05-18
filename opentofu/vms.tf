# MWAN-62 (partial): VM 950 (test-mwan) on suburban managed by OpenTofu.
#
# This resource codifies the suburban MWAN testbed VM that mwan-143 created
# via a shell script. It mirrors prod VM 113 with five NICs (mgmt, internal,
# three simulated ISP WANs). The vhost-vsock-pci device that the watchdog
# uses is set by Ansible on the live host; see the MWAN-154 note below.
#
# Live discovery on 2026-05-07 (qm config 950) was the source of truth for
# this resource. Operators import the running VM into Tofu state per
# opentofu/imports.md; the resource shape is set so `tofu plan` after import
# should report no changes (or only cosmetic comment-style diffs).
#
# Out of scope for this slice: suburban LXCs (200, 201, 202, 203) and
# suburban OPNsense VM 101 (opnsense-test). Those land in a follow-up
# MWAN-62 slice.
#
# Schema reference (bpg/proxmox >= 0.70):
#   https://registry.terraform.io/providers/bpg/proxmox/latest/docs/resources/virtual_environment_vm
#
# MWAN-154: the `kvm_arguments` (Proxmox `args`) field is NOT managed by
# Tofu on VM 950 or VM 101. The Proxmox API rejects writes to `args` for
# any actor other than the bare `root@pam` user (no API tokens, no roles
# can bypass it). Ansible owns this field instead; see the VM 950 vsock
# qm-set task in `ansible/playbooks/deploy-mwan-testbed.yml` and the
# matching VM 101 chardev qm-set task. The bpg/proxmox provider leaves
# undeclared fields alone, so live `args` drift will not surface in
# `tofu plan`.

resource "proxmox_virtual_environment_vm" "vm950_test_mwan" {
  provider  = proxmox.suburban
  node_name = "hypervisor"
  vm_id     = 950
  name      = "test-mwan"

  # MWAN-154: `kvm_arguments` (Proxmox `args`) intentionally omitted.
  # The vhost-vsock-pci device is set by the Ansible task
  # "Set vsock device on VM 950 args" in
  # `ansible/playbooks/deploy-mwan-testbed.yml`.
  machine       = "q35"
  scsi_hardware = "virtio-scsi-pci"
  bios          = "seabios"
  on_boot       = false
  started       = true

  # MWAN-62 reconcile (2026-05-08): keyboard_layout, agent.type match live
  # state from `qm config 950` import.
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

  # MWAN-62 reconcile (2026-05-08): cloud-init drive lives on local-lvm per
  # live `qm config 950` even though the boot disk is on local-zfs.
  initialization {
    datastore_id = "local-lvm"

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

# MWAN-62 / MWAN-140: suburban testbed OPNsense VM 101 (opnsense-test).
#
# This VM is the testbed counterpart of the prod OPNsense router. It boots
# from scsi0 on local-zfs (16G) and exposes the mwan-opnsense virtio-serial
# RPC channel via the `args` block owned by Ansible.
#
# NIC layout (MWAN-148 one-port posture):
#   Prod VM 101 carries MANAGEMENT untagged plus four 802.1q VLAN children
#   (vlan0064, vlan0100, vlan0200, vlan0300) on a single physical port
#   (`iavf0`, the PCI VF). The testbed mirrors that one-port posture by
#   attaching the testbed VM 101 to `vmbrtrunk` once. Inside OPNsense the
#   imported config.xml declares MANAGEMENT as the untagged interface on
#   the trunk parent (the testbed device is `vtnet0`), then declares the
#   four VLAN children on top of that same parent. The config.xml
#   transform layer rewrites every prod-side `iavf0` reference to the
#   testbed's matching device name; see
#   `mwan/docs/MWAN-140-config-xml-transform-spec.md`.
#
# A second NIC on `vmbr2` carries the 10.250.250.0/29 +
# 3d06:bad:b01:201::/64 transit link to VM 950 (test-mwan) so BGP can
# establish with the GoBGP speaker on VM 950 and so the testbed OPNsense
# receives a default route via BGP.
#
# MWAN-154: `kvm_arguments` is owned by Ansible, not Tofu. The Proxmox
# API rejects writes to `args` for any actor other than the bare
# `root@pam` user. The Ansible task in
# `ansible/playbooks/deploy-mwan-testbed.yml` writes the chardev pattern
# to live VM 101. The chardev path is `/var/run/qemu-server/101.mwanrpc`
# and the chardev name `io.goodkind.mwan-opnsense.0` matches what the
# OPNsense plugin opens on `/dev/ttyV0.0` inside the guest.

resource "proxmox_virtual_environment_vm" "opnsense_test" {
  provider  = proxmox.suburban
  node_name = "hypervisor"
  vm_id     = 101
  name      = "opnsense-test"

  scsi_hardware = "virtio-scsi-pci"
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
    dedicated = 4096
  }

  serial_device {
    device = "socket"
  }

  vga {
    type = "serial0"
  }

  # Boot disk. 16G scsi0 on local-zfs sized to fit the OPNsense install
  # plus working space for config history, FRR, and log retention.
  disk {
    datastore_id = "local-zfs"
    interface    = "scsi0"
    size         = 16
  }

  # net0: trunk NIC per MWAN-148. MANAGEMENT untagged plus the four
  # VLAN children share this one port. MAC pinned from live `qm config
  # 101` so the OPNsense config.xml transform layer can key off the
  # stable device-name `vtnet0` derived from this MAC.
  network_device {
    bridge      = "vmbrtrunk"
    model       = "virtio"
    mac_address = "BC:24:11:7D:6D:87"
  }

  # net1: WAN transit NIC on vmbr2 for the 10.250.250.0/29 +
  # 3d06:bad:b01:201::/64 link to VM 950. MAC pinned from live
  # `qm config 101`.
  network_device {
    bridge      = "vmbr2"
    model       = "virtio"
    mac_address = "BC:24:11:0F:66:FA"
  }

  lifecycle {
    prevent_destroy = true
  }
}
