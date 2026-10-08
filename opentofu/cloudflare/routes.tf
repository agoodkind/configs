resource "cloudflare_zero_trust_tunnel_cloudflared_route" "berylax_ipv4" {
  account_id         = var.account_id
  network            = "10.230.0.0/24"
  tunnel_id          = "4a216d14-9e77-4da6-b522-46dc7e5b4dca"
  virtual_network_id = "8a034fb6-d06e-4c08-a567-7860e351635e"
  comment            = "Berylax LAN IPv4"
}
