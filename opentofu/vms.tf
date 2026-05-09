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
# suburban OPNsense VM 101. Those land in a follow-up MWAN-62 slice.
#
# Schema reference (bpg/proxmox >= 0.70):
#   https://registry.terraform.io/providers/bpg/proxmox/latest/docs/resources/virtual_environment_vm
#
# MWAN-154: the `kvm_arguments` (Proxmox `args`) field is NOT managed by
# Tofu on VM 950 or VM 102. The Proxmox API rejects writes to `args` for
# any actor other than the bare `root@pam` user (no API tokens, no roles
# can bypass it). Ansible owns this field instead; see the VM 950 vsock
# qm-set task in `ansible/playbooks/deploy-mwan-testbed.yml` and the
# matching VM 102 chardev qm-set task. The bpg/proxmox provider leaves
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
# from scsi0 on local-zfs (8G) and exposes the mwan-opnsense virtio-serial
# RPC channel via the `args` block.
#
# NIC layout (MWAN-140 slice 2, per MWAN-148 one-port posture):
#   Prod VM 101 carries MANAGEMENT untagged plus four 802.1q VLAN children
#   (vlan0064, vlan0100, vlan0200, vlan0300) on a single physical port
#   (`iavf0`, the PCI VF). The testbed mirrors that one-port posture by
#   attaching VM 101 to `vmbrtrunk` exactly once. Inside OPNsense the
#   imported config.xml declares MANAGEMENT as the untagged interface on
#   the trunk parent (the testbed device is whatever the guest sees, e.g.
#   `vtnet0`), then declares the four VLAN children on top of that same
#   parent. The config.xml transform layer (MWAN-140 slice 4) rewrites
#   every prod-side reference to `iavf0` to the testbed's matching device
#   name; see `mwan/docs/MWAN-140-config-xml-transform-spec.md`.
#
#   MWAN-148 dropped the previously planned separate management bridge
#   (`vmbrmgmt`) and the FreeBSD `rc.conf` rename approach. Both layers
#   are gone: the bridge layer (networks.tf) only defines `vmbrtrunk`,
#   and VM 101 only attaches to `vmbrtrunk`.
#
# Live state caveat (handed off 2026-05-08): VM 101 is wedged from the
# MWAN-119 v2 rollback. The resource definition here documents the
# TARGET shape that slice 6 of MWAN-140 will rebuild from scratch on a
# new VM, not the current live shape. Do not `tofu apply` this resource
# against the wedged VM 101; the rebuild lives in a separate slice and
# uses a fresh OPNsense install per the from-scratch runbook.
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

  # MWAN-62 reconcile (2026-05-08): keyboard_layout and agent.type match
  # live `qm config 101` from suburban. The MWAN-140 slice 2 single-trunk
  # NIC redesign (vmbrtrunk) has not been applied to live yet; the live VM
  # still has the pre-MWAN-140 two-NIC layout (vmbr3 + vmbr2) and no
  # operating_system block. HCL below mirrors live so plan is zero-diff.
  # Slice 6 of MWAN-140 will rebuild VM 101 from scratch on a fresh disk
  # against the one-port `vmbrtrunk` shape; that work is out of scope for
  # this reconciliation.
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

  # net0 + net1: pre-MWAN-140 two-NIC layout currently on the live VM.
  # net0 attaches to vmbr3 (LAN side); net1 attaches to vmbr2 (internal
  # link to VM 950). MAC addresses come from live `qm config 101`. The
  # MWAN-140 slice 6 rebuild collapses these to a single vmbrtrunk NIC,
  # but that has not been applied; the resource records what is live.
  network_device {
    bridge      = "vmbr3"
    model       = "virtio"
    mac_address = "BC:24:11:5A:2E:A0"
  }

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

# MWAN-149: replacement OPNsense testbed VM 102 (opnsense-test2).
#
# Replacement for wedged VM 101. After `tofu apply` creates the shell,
# install OPNsense via serial console per
# `mwan/docs/runbooks/opnsense-serial-vm-from-scratch.md`. Once running,
# install the mwan-opnsense daemon and bring the gRPC channel up. Then
# this VM becomes the testbed baseline for MWAN-127 config import
# rehearsal and MWAN-13 26.x upgrade validation.
#
# VM 101 stays in place as a forensic artifact of the MWAN-119 v2
# rollback wedge. Do not destroy or apply against it; this slice only
# adds VM 102 alongside.
#
# NIC layout follows the MWAN-148 one-port posture: a single NIC on
# `vmbrtrunk` carries MANAGEMENT untagged plus the four 802.1q VLAN
# children (vlan0064, vlan0100, vlan0200, vlan0300). The OPNsense
# config.xml transform layer (see
# `mwan/docs/MWAN-140-config-xml-transform-spec.md`) rewrites prod-side
# `iavf0` references to the testbed's matching device name (typically
# `vtnet0` for the first virtio NIC).
#
# MWAN-154 (2026-05-08): the `args` field is owned by Ansible, not Tofu.
# The Ansible task "Set mwanrpc chardev on VM 102 args" in
# `ansible/playbooks/deploy-mwan-testbed.yml` writes the chardev pattern
# to live VM 102. The chardev path is `/var/run/qemu-server/102.mwanrpc`
# so it does not collide with VM 101. The chardev name
# `io.goodkind.mwan-opnsense.0` matches what the OPNsense plugin opens
# on `/dev/ttyV0.0` inside the guest.
#
# CPU, memory, BIOS, machine, scsi_hardware, agent, serial_device, and
# vga settings mirror VM 101 so the OPNsense installer behaves the same.
#
# MWAN-149 reconcile (2026-05-08): live `qm config 102` on suburban shows
# memory=4096 and scsi0 size=16G after the OPNsense installer expanded
# the disk and the operator increased RAM. HCL below mirrors live so
# `tofu plan` reports zero diff for VM 102.

resource "proxmox_virtual_environment_vm" "opnsense_test2" {
  provider  = proxmox.suburban
  node_name = "hypervisor"
  vm_id     = 102
  name      = "opnsense-test2"

  # MWAN-154: `kvm_arguments` (Proxmox `args`) intentionally omitted.
  # The virtio-serial-pci + mwanrpc chardev block is set by the Ansible
  # task "Set mwanrpc chardev on VM 102 args" in
  # `ansible/playbooks/deploy-mwan-testbed.yml`. See
  # `mwan/docs/proxmox-args-privilege-research-2026-05-08.md` for why
  # API-token writes to `args` cannot succeed.
  scsi_hardware = "virtio-scsi-pci"
  on_boot       = false
  started       = false

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

  # Boot disk. Live size is 16G after the OPNsense installer expanded
  # the volume during the from-scratch install.
  disk {
    datastore_id = "local-zfs"
    interface    = "scsi0"
    size         = 16
  }

  # net0: trunk NIC per MWAN-148. MANAGEMENT untagged plus the four
  # VLAN children share this one port. MAC address is left to the
  # provider so the operator does not have to pre-allocate one; the
  # OPNsense config.xml transform layer keys off device name, not MAC.
  network_device {
    bridge      = "vmbrtrunk"
    model       = "virtio"
    mac_address = "BC:24:11:7D:6D:87"
  }

  # net1 (MWAN-168): WAN transit NIC on vmbr2 carrying the
  # 10.250.250.0/29 + 3d06:bad:b01:201::/64 link to VM 950 (test-mwan).
  # Required so BGP can establish with the GoBGP speaker on VM 950 and
  # so the testbed OPNsense receives a default route via BGP. MAC
  # captured from `qm config 102` on 2026-05-08 after a hot-add via
  # `qm set 102 --net1 virtio,bridge=vmbr2`.
  network_device {
    bridge      = "vmbr2"
    model       = "virtio"
    mac_address = "BC:24:11:D6:38:49"
  }

  lifecycle {
    prevent_destroy = true
  }
}
