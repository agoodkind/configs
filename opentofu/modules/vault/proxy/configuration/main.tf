module "proxy" { source = "../../../configuration/services/proxy" }
module "vault_hypervisor" { source = "../../../configuration/services/vault_hypervisor" }
locals {
  managed_guests = {
    proxy = module.proxy.value
  }
  service_mapping = merge(local.managed_guests, {
    vault_hypervisor = module.vault_hypervisor.value
  })
}
output "service_mapping" { value = local.service_mapping }
locals {
  lock_targets = {
    "module_hosts" = {
      "module.vault" = "vault"
    }
  }
}
module "vault_host" { source = "../../../configuration/hosts/vault" }
output "lock_targets" { value = merge(local.lock_targets, { lock_endpoints = {
  "vault" = module.vault_host.lock_endpoint
} }) }
output "proxy_public_key" { value = module.proxy.upstream_public_key }
output "proxy_address" { value = module.proxy.value.ipv6 }

output "managed_guests" { value = local.managed_guests }
