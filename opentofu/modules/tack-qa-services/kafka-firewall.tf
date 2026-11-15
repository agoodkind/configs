resource "pveguest_file" "kafka_firewall" {
  node = var.data_guest.node
  vmid = var.data_guest.vmid
  kind = var.data_guest.kind
  path = "/etc/tack-qa/kafka.nft"
  content = templatefile("${path.module}/files/kafka.nft.tftpl", {
    application_address = var.application_address
    data_address        = var.data_guest.ipv6
    operator_subnet     = var.data_guest.subnet
  })
}

resource "pveguest_file" "kafka_firewall_unit" {
  node    = var.data_guest.node
  vmid    = var.data_guest.vmid
  kind    = var.data_guest.kind
  path    = "/etc/systemd/system/tack-qa-kafka.service"
  content = file("${path.module}/files/tack-qa-kafka.service")
}

resource "pveguest_systemd_unit" "kafka_firewall" {
  node    = var.data_guest.node
  vmid    = var.data_guest.vmid
  kind    = var.data_guest.kind
  name    = "tack-qa-kafka.service"
  enabled = true
  active  = true
  restart_on = {
    rules        = pveguest_file.kafka_firewall.write_id
    unit         = pveguest_file.kafka_firewall_unit.write_id
    docker_order = pveguest_file.docker_firewall_order.write_id
  }
  depends_on = [pveguest_apt_packages.bootstrap]
}

resource "pveguest_file" "docker_firewall_order" {
  node    = var.data_guest.node
  vmid    = var.data_guest.vmid
  kind    = var.data_guest.kind
  path    = "/etc/systemd/system/docker.service.d/tack-qa-kafka.conf"
  content = file("${path.module}/files/docker-kafka.conf")
}
