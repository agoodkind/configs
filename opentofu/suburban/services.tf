# Guest identity comes from the Ansible service mapping, which is the repository's
# single source of truth for hostnames, VMIDs, IP addresses, and pinned MAC
# addresses. Reading it here keeps OpenTofu and Ansible from drifting apart on
# the same fact. A renumber then changes one line instead of both tools.
#
# A VMID change is a rename on the hypervisor plus state reattachment, never a
# destroy. See ../backend.md#reattach-a-renumbered-guest.
locals {
  service_mapping = yamldecode(
    file("${path.module}/../../ansible/inventory/group_vars/all/service_mapping.yml")
  ).service_mapping
}
