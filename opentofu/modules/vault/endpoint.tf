module "identity" { source = "./services/vault_hypervisor" }
output "lock_endpoint" { value = { user = "root", address = module.identity.value.ipv6 } }
