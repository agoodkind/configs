module "opnsense_suburban" { source = "../../services/opnsense_suburban" }
locals {
  service_mapping = {
    opnsense_suburban = module.opnsense_suburban.value
  }
}
output "service_mapping" { value = local.service_mapping }
locals {
  lock_targets = {
    "module_hosts" = {
      "module.suburban" = "suburban"
    }
  }
}
module "suburban_host" { source = "../.." }
output "lock_targets" { value = merge(local.lock_targets, { lock_endpoints = {
  "suburban" = module.suburban_host.lock_endpoint
} }) }
