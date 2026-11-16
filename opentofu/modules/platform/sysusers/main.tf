resource "pveguest_file" "configuration" {
  node = var.guest.node
  vmid = var.guest.vmid
  kind = var.guest.kind
  path = "/etc/sysusers.d/${var.identity.name}.conf"
  content = join("\n", [
    "g ${var.identity.name} ${var.identity.id}",
    "u! ${var.identity.name} ${var.identity.id}:${var.identity.name} - - /usr/sbin/nologin",
    ""
  ])
}

resource "pveguest_file" "unit" {
  node = var.guest.node
  vmid = var.guest.vmid
  kind = var.guest.kind
  path = "/etc/systemd/system/sysusers-${var.identity.name}.service"
  content = join("\n", [
    "[Unit]", "Description=Create system account ${var.identity.name}",
    "[Service]", "Type=oneshot", "RemainAfterExit=yes",
    "ExecStart=/usr/bin/systemd-sysusers /etc/sysusers.d/${var.identity.name}.conf",
    "[Install]", "WantedBy=multi-user.target", ""
  ])
}

resource "pveguest_systemd_unit" "activation" {
  node    = var.guest.node
  vmid    = var.guest.vmid
  kind    = var.guest.kind
  name    = "sysusers-${var.identity.name}.service"
  enabled = true
  active  = true
  restart_on = {
    configuration = pveguest_file.configuration.write_id
    unit          = pveguest_file.unit.write_id
  }
}
