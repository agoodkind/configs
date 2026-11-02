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

