provider "cloudflare" {
  alias     = "tunnel_routes"
  api_token = var.vault_cloudflare_tunnel_routes_api_token
}

provider "cloudflare" {
  alias     = "tunnels"
  api_token = var.vault_cloudflare_tunnel_api_token
}

provider "cloudflare" {
  alias     = "zero_trust"
  api_token = var.vault_cloudflare_zero_trust_api_token
}

terraform {
  required_version = ">= 1.11"
  required_providers {
    cloudflare = { source = "cloudflare/cloudflare", version = ">= 5.0.0" }
  }
}
