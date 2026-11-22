resource "pveguest_download" "compose_network" {
  node   = local.k3s_kafka_guest.node
  vmid   = local.k3s_kafka_guest.vmid
  kind   = local.k3s_kafka_guest.kind
  url    = "https://raw.githubusercontent.com/agoodkind/tack/7a8489d9e5b3e43f602bccd3927f11d9d2c7be14/docker-compose.yml"
  sha256 = "72b546aa941f9bd169d8aa5195055e1ac7ce8daffca7d848b4d5aa38eb08985a"
  fetch  = "controller"
  path   = "/root/tack/docker-compose.yml"
  mode   = "0644"
}

resource "pveguest_file" "compose_network_unit" {
  node    = local.k3s_kafka_guest.node
  vmid    = local.k3s_kafka_guest.vmid
  kind    = local.k3s_kafka_guest.kind
  path    = "/etc/systemd/system/tack-compose-network.service"
  mode    = "0644"
  content = templatefile("${path.module}/../../modules/tack/application/files/tack-compose-network.service.tftpl", local.docker_ipam)
}

resource "pveguest_file" "compose_network_script" {
  node    = local.k3s_kafka_guest.node
  vmid    = local.k3s_kafka_guest.vmid
  kind    = local.k3s_kafka_guest.kind
  path    = "/usr/local/sbin/tack-compose-network"
  mode    = "0755"
  content = file("${path.module}/../../modules/tack/application/files/tack-compose-network.sh")
}

resource "pveguest_systemd_unit" "compose_network" {
  node    = local.k3s_kafka_guest.node
  vmid    = local.k3s_kafka_guest.vmid
  kind    = local.k3s_kafka_guest.kind
  name    = "tack-compose-network.service"
  enabled = false
  active  = true

  restart_on = {
    compose = pveguest_download.compose_network.write_id
    script  = pveguest_file.compose_network_script.write_id
    unit    = pveguest_file.compose_network_unit.write_id
  }

  depends_on = [
    pveguest_file.k3s_kafka_unit,
    pveguest_file.k3s_kafka_overlay,
    pveguest_systemd_unit.k3s_kafka_firewall,
  ]
}
