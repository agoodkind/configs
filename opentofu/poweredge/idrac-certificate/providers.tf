terraform {
  required_version = ">= 1.12.6"
  required_providers {
    acme       = { source = "vancluever/acme", version = "3.2.1" }
    cloudflare = { source = "cloudflare/cloudflare", version = "5.28.1" }
    http       = { source = "hashicorp/http", version = "3.5.0" }
    restful    = { source = "magodo/restful", version = "0.25.2" }
    time       = { source = "hashicorp/time", version = "0.13.1" }
    tls        = { source = "hashicorp/tls", version = "4.1.0" }
  }
}

provider "cloudflare" {}

provider "acme" {
  server_url = "https://acme-v02.api.letsencrypt.org/directory"
}

provider "restful" {
  base_url = var.idrac_endpoint
  client = {
    certificates              = []
    tls_insecure_skip_verify  = false
    root_ca_certificate_files = var.idrac_initial_ca_file == "" ? null : [var.idrac_initial_ca_file]
  }
  security = {
    http = {
      basic = {
        username = "tofu-certificate"
        password = var.vault_poweredge_idrac_certificate_password
      }
    }
  }
}
