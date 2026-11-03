resource "proxmox_virtual_environment_dns" "vault" {
  node_name = "vault"
  domain    = "home.goodkind.io"
  servers   = ["45.90.28.0", "2a07:a8c0::7d:698e"]
}
