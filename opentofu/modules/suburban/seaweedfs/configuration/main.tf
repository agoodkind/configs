module "suburban_host" { source = "../../../configuration/hosts/suburban" }
output "lock_targets" {
  value = {
    module_hosts   = { "module.base" = "suburban", "module.suburban" = "suburban" }
    node_hosts     = { suburban = "suburban" }
    lock_endpoints = { suburban = module.suburban_host.lock_endpoint }
  }
}

output "managed_guests" { value = {} }
output "service_mapping" { value = {} }
