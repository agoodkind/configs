output "guest" {
  value = {
    node_name         = "vault"
    bridge            = "vmnet"
    dns_servers       = ["45.90.28.0", "2a07:a8c0::7d:698e"]
    datastore_id      = "local-lvm"
    image_datastore   = "local"
    snippet_datastore = "local"
  }
}
