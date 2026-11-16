module "identity" { source = "./services/poweredge_hypervisor" }
output "lock_endpoint" { value = { user = "root", address = module.identity.value.ipv6 } }
