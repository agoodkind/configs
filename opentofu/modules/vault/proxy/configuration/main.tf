module "proxy" { source = "../../services/proxy" }
module "vault_hypervisor" { source = "../../services/vault_hypervisor" }
locals {
  service_mapping = {
    proxy            = module.proxy.value
    vault_hypervisor = module.vault_hypervisor.value
  }
}
output "service_mapping" { value = local.service_mapping }
locals {
  lock_targets = {
    "module_hosts" = {
      "module.vault" = "vault"
    }
  }
}
module "vault_host" { source = "../.." }
output "lock_targets" { value = merge(local.lock_targets, { lock_endpoints = {
  "vault" = module.vault_host.lock_endpoint
} }) }
output "proxy_public_key" { value = module.proxy.upstream_public_key }
output "proxy_address" { value = module.proxy.value.ipv6 }
