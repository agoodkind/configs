locals {
  node = { key = "vault", name = "vault" }
}

output "node" { value = local.node }

output "guest" {
  value = {
    node_name         = local.node.name
    bridge            = "vmnet"
    dns_servers       = ["45.90.28.0", "2a07:a8c0::7d:698e"]
    datastore_id      = "local-lvm"
    image_datastore   = "local"
    snippet_datastore = "local"
  }
}
