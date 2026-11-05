provider "cloudflare" {
  api_token = var.vault_cloudflare_api_token
}

provider "cloudflare" {
  alias     = "dns"
  api_token = var.vault_cloudflare_api_token
}

provider "cloudflare" {
  alias     = "dns_silver"
  api_token = var.vault_cloudflare_silver_flare_dns_api_token
}

provider "cloudflare" {
  alias     = "dns_tack"
  api_token = var.vault_cloudflare_tack_dns_api_token
}

terraform {
  required_version = ">= 1.11"
  required_providers {
    cloudflare = { source = "cloudflare/cloudflare", version = ">= 5.0.0" }
  }
}
