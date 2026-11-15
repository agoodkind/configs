module "suburban_host" { source = "../.." }
output "lock_targets" {
  value = {
    module_hosts   = { "module.search_caller" = "suburban", "module.tack_k3s" = "suburban" }
    node_hosts     = { suburban = "suburban" }
    lock_endpoints = { suburban = module.suburban_host.lock_endpoint }
  }
}
