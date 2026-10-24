provider "cloudflare" {
  api_token = var.vault_cloudflare_api_token
}

locals {
  cloudflare_dns_api_tokens = {
    "goodkind.io"      = var.vault_cloudflare_api_token
    "silver-flare.com" = var.vault_cloudflare_silver_flare_dns_api_token
  }
}

provider "cloudflare" {
  alias     = "dns"
  for_each  = toset(["goodkind.io", "silver-flare.com"])
  api_token = local.cloudflare_dns_api_tokens[each.key]
}

terraform {
  required_version = ">= 1.11"
  required_providers {
    cloudflare = { source = "cloudflare/cloudflare", version = ">= 5.0.0" }
  }
}
