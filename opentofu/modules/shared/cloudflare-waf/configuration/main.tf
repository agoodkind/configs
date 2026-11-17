locals {
  service_mapping = {

  }
}
output "service_mapping" { value = local.service_mapping }
locals {
  lock_targets = {
    "lock_free_types" = ["cloudflare_ruleset"]
  }
}
output "lock_targets" { value = local.lock_targets }
