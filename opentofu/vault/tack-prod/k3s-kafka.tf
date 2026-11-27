locals {
  k3s_kafka_guest        = local.guests["tack"]
  k3s_kafka_address      = cidrhost(local.service_mapping.tack.docker_v6_subnet, 4)
  audit_consumer_address = cidrhost(local.service_mapping.tack.docker_v6_subnet, 5)
  k3s_kafka_compose = templatefile("${path.module}/files/k3s-kafka.compose.yml.tftpl", {
    subnet           = local.service_mapping.tack.docker_v6_subnet
    dynamic_range    = cidrsubnet(local.service_mapping.tack.docker_v6_subnet, 1, 1)
    kafka_address    = local.k3s_kafka_address
    consumer_address = local.audit_consumer_address
  })
}

resource "pveguest_file" "k3s_kafka_overlay" {
  node    = local.k3s_kafka_guest.node
  vmid    = local.k3s_kafka_guest.vmid
  kind    = local.k3s_kafka_guest.kind
  path    = "/root/tack/docker-compose.k3s-kafka.yml"
  content = local.k3s_kafka_compose
}

resource "pveguest_file" "k3s_kafka_firewall" {
  node     = local.k3s_kafka_guest.node
  vmid     = local.k3s_kafka_guest.vmid
  kind     = local.k3s_kafka_guest.kind
  path     = "/etc/nftables.d/tack-k3s-kafka.nft"
  content  = file("${path.module}/files/k3s-kafka.nft")
  validate = "/usr/sbin/nft --check --file %s"
}

resource "pveguest_file" "k3s_kafka_unit" {
  for_each = toset(["tack-k3s-kafka-firewall.service", "tack-k3s-kafka-listener.service"])

  node    = local.k3s_kafka_guest.node
  vmid    = local.k3s_kafka_guest.vmid
  kind    = local.k3s_kafka_guest.kind
  path    = "/etc/systemd/system/${each.value}"
  content = file("${path.module}/../../modules/tack/application/files/${each.value}")
}

resource "pveguest_systemd_unit" "k3s_kafka_firewall" {
  node    = local.k3s_kafka_guest.node
  vmid    = local.k3s_kafka_guest.vmid
  kind    = local.k3s_kafka_guest.kind
  name    = "tack-k3s-kafka-firewall.service"
  enabled = true
  active  = true

  restart_on = {
    rules = pveguest_file.k3s_kafka_firewall.write_id
    unit  = pveguest_file.k3s_kafka_unit["tack-k3s-kafka-firewall.service"].write_id
  }
}

resource "pveguest_systemd_unit" "k3s_kafka_listener" {
  node    = local.k3s_kafka_guest.node
  vmid    = local.k3s_kafka_guest.vmid
  kind    = local.k3s_kafka_guest.kind
  name    = "tack-k3s-kafka-listener.service"
  enabled = true
  active  = true

  restart_on = {
    compose  = pveguest_download.compose_network.write_id
    overlay  = pveguest_file.k3s_kafka_overlay.write_id
    firewall = pveguest_file.k3s_kafka_firewall.write_id
    unit     = pveguest_file.k3s_kafka_unit["tack-k3s-kafka-listener.service"].write_id
  }
}
