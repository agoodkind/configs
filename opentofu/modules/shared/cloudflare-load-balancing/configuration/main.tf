locals {
  service_mapping = {

  }
}
output "service_mapping" { value = local.service_mapping }
locals {
  lock_targets = {
    "lock_free_types" = ["cloudflare_load_balancer", "cloudflare_load_balancer_monitor", "cloudflare_load_balancer_pool"]
  }
}
output "lock_targets" { value = local.lock_targets }
