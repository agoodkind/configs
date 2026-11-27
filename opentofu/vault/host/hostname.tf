locals {
  # Each pattern selects the existing line for one address family that ends
  # with the short host name.
  vault_host_entries = {
    ipv4 = {
      address = split("/", var.management_ipv4_cidr)[0]
      match   = "^[0-9.]+[[:space:]].*[[:space:]]${local.vault_host.hostname}$"
    }
    ipv6 = {
      address = split("/", var.management_ipv6_cidr)[0]
      match   = "^[0-9a-f:]+[[:space:]].*[[:space:]]${local.vault_host.hostname}$"
    }
  }
}

resource "linux_file_line" "hosts" {
  for_each          = local.vault_host_entries
  path              = "/etc/hosts"
  line              = "${each.value.address} ${local.vault_host.fqdn} ${local.vault_host.hostname}"
  match             = each.value.match
  remove_on_destroy = false
}

resource "linux_hostname" "vault" {
  name       = local.vault_host.hostname
  depends_on = [linux_file_line.hosts]
}
