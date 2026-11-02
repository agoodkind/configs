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

resource "pveguest_systemd_unit" "named" {
  node = "vault"
  vmid = local.service_mapping.dns64.vmid
  kind = "lxc"

  name    = "named.service"
  enabled = true
  active  = true

  restart_on = { dependency = pveguest_file.named_dependency.write_id }

  depends_on = [pveguest_systemd_unit.resolved]
}
