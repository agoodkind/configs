module "proxy" { source = "../../../vault/services/proxy" }
locals {
  service_mapping = {
    proxy = module.proxy.value
  }
}
output "service_mapping" { value = local.service_mapping }
locals {
  lock_targets = {
    "lock_free_types" = ["cloudflare_zero_trust_device_custom_profile", "cloudflare_zero_trust_device_managed_networks", "cloudflare_zero_trust_tunnel_cloudflared", "cloudflare_zero_trust_tunnel_cloudflared_route", "cloudflare_zero_trust_tunnel_cloudflared_config"]
  }
}
output "lock_targets" { value = local.lock_targets }
output "proxy_public_key" { value = module.proxy.upstream_public_key }
output "proxy_address" { value = module.proxy.value.ipv6 }
