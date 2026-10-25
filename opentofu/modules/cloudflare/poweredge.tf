locals {
  poweredge_tunnels = {
    host = {
      name = "home-poweredge"
      routes = {
        "10.230.0.254/32"           = "PowerEdge management IPv4"
        "3d06:bad:b01:300::254/128" = "PowerEdge management IPv6"
      }
    }
    mwan = {
      name = "home-mwan-poweredge"
      routes = {
        "10.230.230.1/32" = "PowerEdge MWAN IPv4"
        "fd06:fab::1/128" = "PowerEdge MWAN IPv6"
      }
    }
  }

  poweredge_routes = merge([
    for connector, tunnel in local.poweredge_tunnels : {
      for network, comment in tunnel.routes : network => {
        connector = connector
        comment   = comment
      }
    }
  ]...)
}

resource "cloudflare_zero_trust_tunnel_cloudflared" "poweredge" {
  provider = cloudflare.tunnels
  for_each = local.poweredge_tunnels

  account_id = var.account_id
  name       = each.value.name
  config_src = "cloudflare"
}

resource "cloudflare_zero_trust_tunnel_cloudflared_config" "poweredge" {
  provider = cloudflare.tunnels
  for_each = local.poweredge_tunnels

  account_id = var.account_id
  tunnel_id  = cloudflare_zero_trust_tunnel_cloudflared.poweredge[each.key].id
  source     = "cloudflare"
  config = {
    ingress = [{ service = "http_status:404" }]
  }
}

resource "cloudflare_zero_trust_tunnel_cloudflared_route" "poweredge" {
  provider = cloudflare.routes
  for_each = local.poweredge_routes

  account_id         = var.account_id
  network            = each.key
  tunnel_id          = cloudflare_zero_trust_tunnel_cloudflared.poweredge[each.value.connector].id
  virtual_network_id = "8a034fb6-d06e-4c08-a567-7860e351635e"
  comment            = each.value.comment

  depends_on = [cloudflare_zero_trust_tunnel_cloudflared_config.poweredge]
}

output "poweredge_tunnel_ids" {
  description = "PowerEdge tunnel identifiers keyed by host or mwan."
  value       = { for name, tunnel in cloudflare_zero_trust_tunnel_cloudflared.poweredge : name => tunnel.id }
}
