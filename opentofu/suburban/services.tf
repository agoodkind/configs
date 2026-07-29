# Guest identity comes from the Ansible service mapping, which is the repository's
# single source of truth for hostnames, VMIDs, and addresses. Reading it here
# keeps OpenTofu and Ansible from drifting apart on the same fact, and makes a
# renumber a one-line change in that file rather than a sweep through both tools.
#
# A VMID change is a rename on the hypervisor plus a state re-attach, never a
# destroy. See ../imports.md.
locals {
  service_mapping = yamldecode(
    file("${path.module}/../../ansible/inventory/group_vars/all/service_mapping.yml")
  ).service_mapping
}
