locals {
  node = { key = "suburban", name = "hypervisor" }
}

output "node" { value = local.node }

output "guest" {
  value = {
    node_name         = local.node.name
    bridge            = local.vmnet.bridge
    dns_servers       = ["2a07:a8c0::7d:698e", "2a07:a8c1::7d:698e"]
    datastore_id      = "local-zfs"
    image_datastore   = "local"
    snippet_datastore = "local"
  }
}
