locals {
  legacy_proxy_origin = "h2c://[3d06:bad:b01:0:7ac::2]:8000"
}

resource "pveguest_file" "legacy_proxy_origin_script" {
  node = "vault"
  vmid = 110
  kind = "lxc"

  path    = "/usr/local/sbin/tack-legacy-proxy-origin.py"
  content = file("${path.module}/files/tack-legacy-proxy-origin.py")
  mode    = "0755"
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
    "ExecStart=/usr/bin/python3 /usr/local/sbin/tack-legacy-proxy-origin.py",
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
    script = pveguest_file.legacy_proxy_origin_script.write_id
  }

  depends_on = [
    pveguest_systemd_unit.compose_network,
    pveguest_systemd_unit.k3s_kafka_listener,
    module.tack_release,
  ]
}
