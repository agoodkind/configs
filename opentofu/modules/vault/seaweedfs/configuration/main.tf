module "seaweedfs" { source = "../../services/seaweedfs" }
module "vault_hypervisor" { source = "../../services/vault_hypervisor" }
locals {
  service_mapping = {
    seaweedfs        = module.seaweedfs.value
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
