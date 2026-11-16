output "guest" {
  value = {
    node_name         = "hypervisor"
    bridge            = local.bridges.trunk_suburban
    dns_servers       = ["2a07:a8c0::7d:698e", "2a07:a8c1::7d:698e"]
    datastore_id      = "local-zfs"
    image_datastore   = "local"
    snippet_datastore = "local"
  }
}
