locals {
  service_mapping = {

  }
}
output "service_mapping" { value = local.service_mapping }
locals {
  lock_targets = {
    "module_hosts" = {
      "module.poweredge" = "poweredge"
    }
  }
}
module "poweredge_host" { source = "../.." }
output "lock_targets" { value = merge(local.lock_targets, { lock_endpoints = {
  "poweredge" = module.poweredge_host.lock_endpoint
} }) }
