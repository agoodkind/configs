locals {
  units = {
    data = join("\n", [
      "[Unit]", "Description=Start fresh Tack QA data services", "After=docker.service tack-qa-kafka.service", "Requires=docker.service tack-qa-kafka.service",
      "[Service]", "Type=oneshot", "RemainAfterExit=yes", "WorkingDirectory=/opt/tack-qa/data", "TimeoutStartSec=540",
      "ExecStartPre=/usr/bin/mkdir -p /etc/foundationdb",
      "ExecStartPre=/usr/bin/install -d -o root -g root -m 0755 /opt/tack-qa/ledger",
      "ExecStart=/usr/bin/docker compose up -d fdb kafka",
      "ExecStart=/usr/bin/docker compose up -d --force-recreate yugabyte",
      "ExecStart=/usr/bin/docker compose exec -T yugabyte /usr/bin/env bash /opt/tack/bootstrap.sh",
      "ExecStart=/usr/bin/docker compose up -d --wait --wait-timeout 240 fdb yugabyte kafka",
      "[Install]", "WantedBy=multi-user.target", ""
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
  for_each   = local.units
  node       = local.guests[each.key].node
  vmid       = local.guests[each.key].vmid
  kind       = local.guests[each.key].kind
  name       = "tack-qa-${each.key}.service"
  enabled    = true
  active     = true
  depends_on = [pveguest_systemd_unit.kafka_firewall]
  restart_on = merge(
    { unit = pveguest_file.unit[each.key].write_id, yugabyte_password = pveguest_file.yugabyte_password.write_id },
    { for key, value in pveguest_file.configuration : key => value.write_id },
    { for key, value in pveguest_file.secret : key => value.write_id },
    { for key, value in pveguest_download.overlay : key => value.write_id },
  )
}
resource "pveguest_file" "bootstrap_unit" {
  node = var.data_guest.node
  vmid = var.data_guest.vmid
  kind = var.data_guest.kind
  path = "/etc/systemd/system/tack-qa-bootstrap.service"
  content = join("\n", [
    "[Unit]", "Description=Initialize fresh Tack QA databases and search model", "After=tack-qa-data.service", "Requires=tack-qa-data.service",
    "[Service]", "Type=oneshot", "RemainAfterExit=yes", "TimeoutStartSec=280", "ExecStart=/opt/tack-qa/data/bootstrap.sh", ""
  ])
}
resource "pveguest_systemd_unit" "bootstrap" {
  node       = var.data_guest.node
  vmid       = var.data_guest.vmid
  kind       = var.data_guest.kind
  name       = "tack-qa-bootstrap.service"
  enabled    = false
  active     = true
  restart_on = { search = var.search_ready, unit = pveguest_file.bootstrap_unit.write_id, script = pveguest_file.configuration["data/bootstrap.sh"].write_id }
  depends_on = [pveguest_systemd_unit.runtime, pveguest_apt_packages.bootstrap]
}

resource "pveguest_apt_packages" "bootstrap" {
  node     = var.data_guest.node
  vmid     = var.data_guest.vmid
  kind     = var.data_guest.kind
  packages = ["jq", "nftables"]
}
