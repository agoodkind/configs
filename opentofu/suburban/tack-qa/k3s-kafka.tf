locals {
  k3s_kafka_guest = local.guests["tack_qa_suburban"]
}

resource "pveguest_file" "k3s_kafka_overlay" {
  node    = local.k3s_kafka_guest.node
  vmid    = local.k3s_kafka_guest.vmid
  kind    = local.k3s_kafka_guest.kind
  path    = "/root/tack/docker-compose.k3s-kafka.yml"
  content = file("${path.module}/files/k3s-kafka.compose.yml")
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
  content = file("${path.module}/../../modules/tack-k3s/files/${each.value}")
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
    overlay  = pveguest_file.k3s_kafka_overlay.write_id
    firewall = pveguest_file.k3s_kafka_firewall.write_id
    unit     = pveguest_file.k3s_kafka_unit["tack-k3s-kafka-listener.service"].write_id
  }

  depends_on = [pveguest_systemd_unit.k3s_kafka_firewall]
}
