module "terrakube" { source = "../../../vault/terrakube-prod/configuration" }

output "webhook" { value = module.terrakube.webhook }
output "service_mapping" { value = {} }
output "managed_guests" { value = {} }
output "lock_targets" {
  value = {
    lock_free_types = ["cloudflare_certificate_pack", "cloudflare_dns_record"]
  }
}
