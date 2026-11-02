locals {
  dns64 = yamldecode(file("${local.repository_root}/ansible/inventory/group_vars/dns64_servers.yml"))
  named_configuration = templatefile("${path.module}/named.conf.options.tftpl", {
    upstream             = local.dns64.dns64_upstream_dns
    prefix               = local.dns64.dns64_prefix
    force_synth          = local.dns64.dns64_force_synth
    native_ipv6_prefixes = local.dns64.dns64_native_ipv6_prefixes
  })
}

resource "pveguest_apt_packages" "dns" {
  node = "vault"
  vmid = local.service_mapping.dns64.vmid
  kind = "lxc"

  packages = ["bind9", "systemd-resolved"]

  depends_on = [module.vault]
}

resource "pveguest_file" "nextdns" {
  node = "vault"
  vmid = local.service_mapping.dns64.vmid
  kind = "lxc"

  path    = "/etc/systemd/resolved.conf.d/20-nextdns.conf"
  content = file("${path.module}/nextdns.conf")

  depends_on = [pveguest_apt_packages.dns]
}

resource "pveguest_systemd_unit" "resolved" {
  node = "vault"
  vmid = local.service_mapping.dns64.vmid
  kind = "lxc"

  name    = "systemd-resolved.service"
  enabled = true
  active  = true

  restart_on = { nextdns = pveguest_file.nextdns.write_id }
}

resource "pveguest_file" "named_dependency" {
  node = "vault"
  vmid = local.service_mapping.dns64.vmid
  kind = "lxc"

  path    = "/etc/systemd/system/named.service.d/nextdns.conf"
  content = file("${path.module}/named-nextdns.conf")

  depends_on = [pveguest_apt_packages.dns]
}

resource "pveguest_file" "named_configuration" {
  node = "vault"
  vmid = local.service_mapping.dns64.vmid
  kind = "lxc"

  path     = "/etc/bind/named.conf.options"
  content  = local.named_configuration
  group    = "bind"
  validate = "/usr/bin/named-checkconf %s"

  depends_on = [pveguest_apt_packages.dns]
}

resource "pveguest_systemd_unit" "named" {
  node = "vault"
  vmid = local.service_mapping.dns64.vmid
  kind = "lxc"

  name    = "named.service"
  enabled = true
  active  = true

  restart_on = {
    dependency    = pveguest_file.named_dependency.write_id
    configuration = pveguest_file.named_configuration.write_id
  }

  depends_on = [pveguest_systemd_unit.resolved]
}
