resource "cloudflare_zero_trust_tunnel_cloudflared_route" "tack_statics" {
  provider = cloudflare.tunnel_routes
  for_each = toset(local.tack_static_routes)

  account_id         = var.cloudflare_account_id
  network            = each.value
  tunnel_id          = cloudflare_zero_trust_tunnel_cloudflared.tack_k3s["prod"].id
  virtual_network_id = "8a034fb6-d06e-4c08-a567-7860e351635e"
  comment            = local.tack_static_route_label
}
