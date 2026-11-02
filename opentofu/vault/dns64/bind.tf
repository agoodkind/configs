locals {
  dns64 = yamldecode(file("${local.repository_root}/ansible/inventory/group_vars/dns64_servers.yml"))
  named_configuration = templatefile("${path.module}/named.conf.options.tftpl", {
    upstream             = local.service_mapping.opnsense.ipv6
    prefix               = local.dns64.dns64_prefix
    force_synth          = local.dns64.dns64_force_synth
    native_ipv6_prefixes = local.dns64.dns64_native_ipv6_prefixes
  })
}

resource "pveguest_file" "named_configuration" {
  node = "vault"
  vmid = local.service_mapping.dns64.vmid
  kind = "lxc"

  path     = "/etc/bind/named.conf.options"
  content  = local.named_configuration
  group    = "bind"
  validate = "/usr/bin/named-checkconf %s"

  depends_on = [module.vault]
}

resource "pveguest_systemd_unit" "named" {
  node = "vault"
  vmid = local.service_mapping.dns64.vmid
  kind = "lxc"

  name    = "named.service"
  enabled = true
  active  = true

  restart_on = { configuration = pveguest_file.named_configuration.write_id }
}
