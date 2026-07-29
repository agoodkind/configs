# Guest identity comes from the Ansible service mapping, which is the repository's
# single source of truth for hostnames, VMIDs, and addresses. Reading it here
# keeps OpenTofu and Ansible from drifting apart on the same fact.
locals {
  service_mapping = yamldecode(
    file("${path.module}/../../ansible/inventory/group_vars/all/service_mapping.yml")
  ).service_mapping
}
