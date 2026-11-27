module "terrakube" { source = "../../../vault/terrakube-prod/configuration" }

output "dashboard" { value = module.terrakube.dashboard }
output "service_mapping" { value = {} }
output "managed_guests" { value = {} }
output "lock_targets" {
  value = {
    lock_free_types = [
      "cloudflare_zero_trust_access_policy",
      "cloudflare_zero_trust_device_custom_profile",
      "cloudflare_zero_trust_gateway_policy",
      "cloudflare_zero_trust_tunnel_cloudflared_route",
      "cloudflare_zero_trust_tunnel_cloudflared_virtual_network",
    ]
  }
}
