provider "cloudflare" {
  api_token = var.vault_cloudflare_goodkind_io_waf_api_token
}

terraform {
  required_version = ">= 1.11"
  required_providers {
    cloudflare = { source = "cloudflare/cloudflare", version = ">= 5.0.0" }
  }
}
