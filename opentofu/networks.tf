# MWAN-63: suburban testbed bridges managed by OpenTofu.
#
# The mwan-140 slice 1 design added two bridges to suburban for the OPNsense
# testbed VM (VM 101) to mirror the prod iavf0 VLAN parent shape:
#
#   * trunk: VLAN-aware trunk bridge carrying VIDs 64, 100, 200, 300.
#     VM 101 net3 attaches as a tagged trunk member. OPNsense renames the
#     virtio NIC to iavf0 inside the guest so the imported config.xml VLAN
#     children (vlan0064, vlan0100, vlan0200, vlan0300) bind correctly.
#
#   * mgmt: untagged MANAGEMENT bridge mirroring prod iavf0 native.
#     The host carries 10.240.4.1/24 and 3d06:bad:b01:204::1/64 so suburban
#     itself can reach the MANAGEMENT plane for diagnostics. VM 101 net2
#     attaches here.
#
# Naming constraint (verified at validate time on 2026-05-07): the
# bpg/proxmox provider enforces the upstream Proxmox API rule that bridge
# names match `^[a-zA-Z][a-zA-Z0-9_]*$` and stay at or under 10 characters.
# The mwan-140 spec used `vmbr-trunk` and `vmbr-mgmt`, which violate the
# hyphen and length rules. Live discovery on 2026-05-07 confirmed those
# bridges do NOT exist on suburban yet (the slice 1 Ansible template was
# never applied), so this slice renames them to `vmbrtrunk` and
# `vmbrmgmt`. Downstream callers (mwan-140 fragments, OPNsense config.xml,
# vmbr2/vmbr3 suburban interfaces stanza, ansible group_vars) need to
# follow the same rename in their own slices before applying these
# resources. Surfacing this as a follow-up because it touches scope
# outside MWAN-63/MWAN-62.
#
# Schema reference (bpg/proxmox >= 0.106):
#   https://registry.terraform.io/providers/bpg/proxmox/latest/docs/resources/network_linux_bridge
# The legacy `proxmox_virtual_environment_network_linux_bridge` resource
# is deprecated and slated for removal in v1.0; the new resource name is
# `proxmox_network_linux_bridge`. The `vids` field takes a space-separated
# string of VIDs and ranges.

resource "proxmox_network_linux_bridge" "trunk" {
  provider  = proxmox.suburban
  node_name = "hypervisor"
  name      = "vmbrtrunk"
  comment   = "MWAN-140 slice 1: VLAN-aware trunk for OPNsense iavf0 parity"

  vlan_aware = true
  vids       = "64 100 200 300"
  autostart  = true

  lifecycle {
    prevent_destroy = true
  }
}

resource "proxmox_network_linux_bridge" "mgmt" {
  provider  = proxmox.suburban
  node_name = "hypervisor"
  name      = "vmbrmgmt"
  comment   = "MWAN-140 slice 1: untagged MANAGEMENT bridge mirroring prod iavf0 native"

  address   = "10.240.4.1/24"
  address6  = "3d06:bad:b01:204::1/64"
  autostart = true

  lifecycle {
    prevent_destroy = true
  }
}
