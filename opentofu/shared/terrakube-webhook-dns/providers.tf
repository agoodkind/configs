terraform {
  required_version = ">= 1.11"
  required_providers {
    cloudflare = { source = "cloudflare/cloudflare", version = ">= 5.0.0" }
  }
}

provider "cloudflare" {
  alias     = "certificates"
  api_token = var.vault_cloudflare_goodkind_io_certificates_api_token
}

provider "cloudflare" {
  alias     = "dns"
  api_token = var.vault_cloudflare_api_token
}

provider "cloudflare" {
  alias     = "tunnels"
  api_token = var.vault_cloudflare_tunnel_api_token
}
