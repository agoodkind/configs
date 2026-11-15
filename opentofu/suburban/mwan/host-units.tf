locals {
  mwan_host_unit_inputs = {
    "mwan-ifmgr.service" = [
      "/usr/local/bin/mwan",
      "/etc/mwan/config.toml",
      "/etc/systemd/system/mwan-ifmgr.service",
      "/etc/systemd/system/mwan-ifmgr.service.d/oob-dhcpv4.conf",
      "/etc/systemd/system/mwan-ifmgr.service.d/suburban-host-ipv6-policy.conf",
    ]
  }
}

resource "pveguest_host_systemd_unit" "mwan" {
  for_each = local.mwan_host_unit_inputs

  node       = "suburban"
  workspace  = "suburban/mwan"
  name       = each.key
  enabled    = true
  active     = true
  restart_on = { for path in each.value : path => pveguest_host_artifact.mwan_host.write_ids[path] }

  depends_on = [pveguest_host_secret.smtp2go]
}
