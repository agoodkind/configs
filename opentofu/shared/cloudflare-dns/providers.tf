# The provider reads CLOUDFLARE_API_TOKEN from the environment inherited by configsctl.
provider "cloudflare" {}

terraform {
  required_version = ">= 1.11"
  required_providers {
    cloudflare = { source = "cloudflare/cloudflare", version = ">= 5.0.0" }
  }
}
