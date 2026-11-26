terraform {
  required_version = ">= 1.11"
  required_providers {
    cloudflare = { source = "cloudflare/cloudflare", version = ">= 5.29.0" }
  }
}

provider "cloudflare" {
  alias     = "enrollment"
  api_token = var.vault_cloudflare_terrakube_runner_enrollment_api_token
}

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
