locals {
  service_mapping = {

  }
}
output "service_mapping" { value = local.service_mapping }
locals {
  lock_targets = {
    "lock_free_types" = ["cloudflare_dns_record", "cloudflare_pages_domain", "cloudflare_pages_project"]
  }
}
output "lock_targets" { value = local.lock_targets }

output "managed_guests" { value = {} }
