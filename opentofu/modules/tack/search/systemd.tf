locals {
  units = {
    search = join("\n", [
      "[Unit]", "Description=Start fresh Tack QA search", "After=docker.service", "Requires=docker.service",
      "[Service]", "Type=oneshot", "RemainAfterExit=yes", "WorkingDirectory=/opt/tack-qa/search", "TimeoutStartSec=280",
      "ExecStartPre=/opt/tack-qa/search/check-host.sh",
      "ExecStartPre=/usr/bin/chown -R 1000:1000 /opt/tack-qa/search",
      "ExecStart=/usr/bin/docker compose up -d opensearch",
      "ExecStartPost=/opt/tack-qa/search/security.sh", "[Install]", "WantedBy=multi-user.target", ""
    ])
  }
}
resource "pveguest_file" "unit" {
  for_each = local.units
  node     = local.guests[each.key].node
  vmid     = local.guests[each.key].vmid
  kind     = local.guests[each.key].kind
  path     = "/etc/systemd/system/tack-qa-${each.key}.service"
  content  = each.value
}
resource "pveguest_systemd_unit" "runtime" {
  for_each = local.units
  node     = local.guests[each.key].node
  vmid     = local.guests[each.key].vmid
  kind     = local.guests[each.key].kind
  name     = "tack-qa-${each.key}.service"
  enabled  = true
  active   = true
  restart_on = merge(
    { unit = pveguest_file.unit[each.key].write_id },
    { for key, value in pveguest_file.configuration : key => value.write_id },
    { for key, value in pveguest_file.secret : key => value.write_id },
    { users = pveguest_file.search_users.write_id }
  )
}
