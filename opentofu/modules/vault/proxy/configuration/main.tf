output "service_mapping" { value = {} }
locals {
  lock_targets = {
    node_hosts = { (module.vault_host.node.key) = "vault" }
    module_hosts = {
      "module.vault" = "vault"
    }
  }
}
module "vault_host" { source = "../../../configuration/hosts/vault" }
output "lock_targets" { value = merge(local.lock_targets, { lock_endpoints = {
  "vault" = module.vault_host.lock_endpoint
} }) }
output "managed_guests" { value = {} }
