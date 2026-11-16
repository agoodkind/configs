provider "cloudflare" {
  alias     = "dns"
  api_token = var.vault_cloudflare_api_token
}

provider "cloudflare" {
  alias     = "pages"
  api_token = var.vault_cloudflare_pages_api_token
}

terraform {
  required_version = ">= 1.11"
  required_providers {
    cloudflare = { source = "cloudflare/cloudflare", version = ">= 5.0.0" }
  }
}
