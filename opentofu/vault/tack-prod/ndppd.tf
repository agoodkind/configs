resource "pveguest_file" "ndppd_config" {
  node = local.guests.tack.node
  vmid = local.guests.tack.vmid
  kind = local.guests.tack.kind
  path = "/etc/ndppd.conf"
  mode = "0644"
  content = templatefile("${path.module}/../../modules/docker-guest/files/ndppd.conf.tftpl", {
    interface = "eth0"
    prefix    = local.service_mapping.tack.docker_v6_subnet
    method    = "static"
  })
}

resource "pveguest_file" "ndppd_unit" {
  node    = local.guests.tack.node
  vmid    = local.guests.tack.vmid
  kind    = local.guests.tack.kind
  path    = "/etc/systemd/system/ndppd.service"
  mode    = "0644"
  content = file("${path.module}/../../modules/docker-guest/files/ndppd.service")
}

resource "pveguest_systemd_unit" "ndppd" {
  node    = local.guests.tack.node
  vmid    = local.guests.tack.vmid
  kind    = local.guests.tack.kind
  name    = "ndppd.service"
  enabled = true
  active  = true

  restart_on = {
    config  = pveguest_file.ndppd_config.write_id
    service = pveguest_file.ndppd_unit.write_id
  }
}
