locals {
  mwan_host_unit_inputs = {
    "mwan-ifmgr.service" = [
      "/usr/local/bin/mwan",
      "/etc/mwan/config.toml",
      "/etc/systemd/system/mwan-ifmgr.service",
      "/etc/systemd/system/mwan-ifmgr.service.d/oob-dhcpv4.conf",
      "/etc/systemd/system/mwan-ifmgr.service.d/suburban-host-ipv6-policy.conf",
    ]
    "mwan-watchdog-testbed.service" = [
      "/usr/local/bin/mwan",
      "/etc/mwan/config.toml",
      "/etc/systemd/system/mwan-watchdog-testbed.service",
    ]
  }

  opnsense_drain_unit_inputs = [
    "/usr/local/bin/opnsensectl",
    "/etc/opnsensectl/config.toml",
    "/etc/systemd/system/mwan-opnsense-drain.service",
  ]

  opnsense_bridge_unit_inputs = [
    "/usr/local/bin/opnsensectl",
    "/etc/opnsensectl/config.toml",
    "/etc/systemd/system/mwan-opnsense-host.service",
  ]
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

resource "pveguest_host_systemd_unit" "opnsense_drain" {
  node       = "suburban"
  workspace  = "suburban/mwan"
  name       = "mwan-opnsense-drain.service"
  enabled    = true
  active     = true
  restart_on = { for path in local.opnsense_drain_unit_inputs : path => pveguest_host_artifact.mwan_host.write_ids[path] }
}

resource "pveguest_host_systemd_unit" "opnsense_bridge" {
  node       = "suburban"
  workspace  = "suburban/mwan"
  name       = "mwan-opnsense-host.service"
  enabled    = true
  active     = true
  restart_on = { for path in local.opnsense_bridge_unit_inputs : path => pveguest_host_artifact.mwan_host.write_ids[path] }

  depends_on = [pveguest_host_systemd_unit.opnsense_drain]
}
