resource "cloudflare_zero_trust_tunnel_cloudflared" "poweredge" {
  provider = cloudflare.tunnels
  for_each = { host = "home-poweredge" }

  account_id = var.account_id
  name       = each.value
  config_src = "cloudflare"

  lifecycle {
    prevent_destroy = true
  }
}
