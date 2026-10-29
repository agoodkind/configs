provider "cloudflare" {
  alias     = "mwan_manage"
  api_token = sensitive(trimspace(file(pathexpand(var.cloudflare_mwan_manage_token_file))))
}

terraform {
  required_version = ">= 1.11"
  required_providers {
    cloudflare = { source = "cloudflare/cloudflare", version = ">= 5.0.0" }
  }
}
