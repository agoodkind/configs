resource "cloudflare_zero_trust_tunnel_cloudflared_route" "home_ipv4" {
  provider = cloudflare.routes

  account_id         = var.account_id
  network            = "10.250.0.0/24"
  tunnel_id          = "50453c03-7d04-40fa-a86e-a8c88e851b78"
  virtual_network_id = "8a034fb6-d06e-4c08-a567-7860e351635e"
  comment            = "Home LAN IPv4"
}

resource "cloudflare_zero_trust_tunnel_cloudflared_route" "home_guests" {
  provider = cloudflare.routes

  account_id         = var.account_id
  network            = "3d06:bad:b01::/64"
  tunnel_id          = "50453c03-7d04-40fa-a86e-a8c88e851b78"
  virtual_network_id = "8a034fb6-d06e-4c08-a567-7860e351635e"
  comment            = "Home VMNET IPv6"
}

resource "cloudflare_zero_trust_tunnel_cloudflared" "router" {
  provider = cloudflare.tunnels

  account_id = var.account_id
  name       = "home-router"
  config_src = "cloudflare"
}

data "cloudflare_zero_trust_tunnel_cloudflared_token" "router" {
  provider = cloudflare.tunnels

  account_id = var.account_id
  tunnel_id  = cloudflare_zero_trust_tunnel_cloudflared.router.id
}

locals {
  router_wireguard_routes = {
    "10.250.10.0/24"       = "Home WireGuard IPv4 clients"
    "3d06:bad:b01:a::/64"  = "Home WireGuard IPv6 clients"
    "3d06:bad:b01:10::/64" = "Home WireGuard legacy IPv6 clients"
  }
}

resource "cloudflare_zero_trust_tunnel_cloudflared_route" "router_wireguard" {
  provider = cloudflare.routes
  for_each = var.router_routes_enabled ? local.router_wireguard_routes : {}

  account_id         = var.account_id
  network            = each.key
  tunnel_id          = cloudflare_zero_trust_tunnel_cloudflared.router.id
  virtual_network_id = "8a034fb6-d06e-4c08-a567-7860e351635e"
  comment            = each.value

}
