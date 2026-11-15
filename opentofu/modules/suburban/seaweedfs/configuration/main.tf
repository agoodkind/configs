module "suburban_host" { source = "../.." }
output "lock_targets" {
  value = {
    module_hosts   = { "module.base" = "suburban", "module.suburban" = "suburban" }
    node_hosts     = { suburban = "suburban" }
    lock_endpoints = { suburban = module.suburban_host.lock_endpoint }
  }
}
