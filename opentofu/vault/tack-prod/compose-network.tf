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
