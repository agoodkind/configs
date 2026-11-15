locals {
  legacy_proxy_origin    = "h2c://[3d06:bad:b01:0:7ac::2]:8000"
  legacy_proxy_old_match = "h2c://[[]3d06:bad:b01::117[]]:8000"
  legacy_proxy_match     = "h2c://[[]3d06:bad:b01:0:7ac::2[]]:8000"
}

resource "pveguest_file" "legacy_proxy_origin_unit" {
  node = "vault"
  vmid = 110
  kind = "lxc"

  path = "/etc/systemd/system/tack-legacy-proxy-origin.service"
  content = join("\n", [
    "[Unit]",
    "Description=Update the legacy Tack Traefik origin",
    "AssertPathExists=/etc/traefik/dynamic/routes.yml",
    "",
    "[Service]",
    "Type=oneshot",
    "RemainAfterExit=yes",
    "TimeoutStartSec=30",
    "ExecStartPre=/usr/bin/grep -q -e ${local.legacy_proxy_old_match} -e ${local.legacy_proxy_match} /etc/traefik/dynamic/routes.yml",
    "ExecStart=/usr/bin/sed -i s|${local.legacy_proxy_old_match}|${local.legacy_proxy_origin}| /etc/traefik/dynamic/routes.yml",
    "ExecStartPost=/usr/bin/grep -F -q ${local.legacy_proxy_origin} /etc/traefik/dynamic/routes.yml",
    "",
  ])
}

resource "pveguest_systemd_unit" "legacy_proxy_origin" {
  node = "vault"
  vmid = 110
  kind = "lxc"

  name    = "tack-legacy-proxy-origin.service"
  enabled = false
  active  = true

  restart_on = {
    unit   = pveguest_file.legacy_proxy_origin_unit.write_id
    origin = local.legacy_proxy_origin
  }

  depends_on = [
    pveguest_systemd_unit.compose_network,
    pveguest_systemd_unit.k3s_kafka_listener,
    module.tack_release,
  ]
}
