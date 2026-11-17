resource "cloudflare_zero_trust_tunnel_cloudflared_config" "proxy" {
  provider = cloudflare.tunnels

  account_id = var.cloudflare_account_id
  tunnel_id  = module.contracts.tack_tunnel_id
  source     = "cloudflare"
  config = {
    ingress = [{ service = "http_status:404" }]
  }
}

import {
  to = cloudflare_zero_trust_tunnel_cloudflared_config.proxy
  id = "${var.cloudflare_account_id}/${module.contracts.tack_tunnel_id}"
}
