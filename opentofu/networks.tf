# MWAN-63: suburban testbed bridges managed by OpenTofu.
#
# The mwan-140 slice 1 design originally added two bridges to suburban for
# the OPNsense testbed VM 101 to mirror the prod iavf0 VLAN parent
# shape. MWAN-148 narrowed that to a single trunk bridge, since prod runs
# MANAGEMENT untagged on the same physical port that carries the VLAN
# trunk (`iavf0`). The testbed mirrors that one-port posture: VM 101
# attaches to the trunk bridge once, and OPNsense exposes MANAGEMENT as
# the untagged interface plus the four 802.1q VLAN children on top.
#
#   * trunk: VLAN-aware trunk bridge carrying VIDs 64, 100, 200, 300.
#     The OPNsense testbed VM 101 attaches as a trunk member that also
#     carries the untagged MANAGEMENT plane. The imported config.xml
#     VLAN children (vlan0064, vlan0100, vlan0200, vlan0300) bind to
#     the testbed equivalent device through the config.xml transform
#     layer rather than via a FreeBSD rename. See MWAN-148 for the
#     rationale.
#
# Naming constraint (verified at validate time on 2026-05-07): the
# bpg/proxmox provider enforces the upstream Proxmox API rule that bridge
# names match `^[a-zA-Z][a-zA-Z0-9_]*$` and stay at or under 10 characters.
# The mwan-140 spec used `vmbr-trunk`, which violates the hyphen rule.
# Live discovery on 2026-05-07 confirmed the bridge does NOT exist on
# suburban yet (the slice 1 Ansible template was never applied), so this
# slice uses `vmbrtrunk`. Downstream callers (mwan-140 fragments,
# OPNsense config.xml, vmbr2/vmbr3 suburban interfaces stanza, ansible
# group_vars) need to follow the same name in their own slices before
# applying this resource.
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

  # Suburban stub IP on the untagged side of vmbrtrunk so this host can
  # reach the OPNsense testbed MANAGEMENT (10.240.4.0/24) and the
  # testbed mwan gRPC unix socket from a single bridge. Mirrors prod
  # vault joining the OPNsense LAN bridge as a stub client.
  address  = "10.240.4.5/24"
  address6 = "3d06:bad:b01:204::5/64"

  lifecycle {
    prevent_destroy = true
  }
}
