terraform {
  required_providers {
    proxmox = {
      source  = "bpg/proxmox"
      version = ">= 0.106.0"
    }
    http = {
      source  = "hashicorp/http"
      version = ">= 3.0"
    }
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = ">= 5.0.0"
    }
  }
  # The data_wo argument of proxmox_acme_dns_plugin is write-only. Write-only
  # arguments require OpenTofu 1.11.
  required_version = ">= 1.11"
}

# The provider reads CLOUDFLARE_API_TOKEN from the environment inherited by configsctl.
provider "cloudflare" {}

provider "cloudflare" {
  alias     = "zero_trust"
  api_token = var.vault_cloudflare_zero_trust_api_token
}

provider "cloudflare" {
  alias     = "tunnel_routes"
  api_token = var.vault_cloudflare_tunnel_routes_api_token
}

provider "cloudflare" {
  alias     = "mwan_manage"
  api_token = sensitive(trimspace(file(pathexpand(var.cloudflare_mwan_manage_token_file))))
}

locals {
  proxmox_token_principal = "${local.shared_vars.proxmox_api_user}!${local.shared_vars.proxmox_token_id}"
}

provider "proxmox" {
  endpoint  = var.proxmox_endpoint
  api_token = "${local.proxmox_token_principal}=${var.vault_proxmox_token_secret}"
  insecure  = true
}

provider "proxmox" {
  alias     = "poweredge"
  endpoint  = var.poweredge_proxmox_endpoint
  api_token = "${local.proxmox_token_principal}=${var.vault_poweredge_pve_token_secret}"
  insecure  = true
}

provider "proxmox" {
  alias     = "suburban"
  endpoint  = var.suburban_proxmox_endpoint
  api_token = "${local.proxmox_token_principal}=${var.vault_suburban_testbed_pve_token_secret}"
  insecure  = true
}

provider "proxmox" {
  alias    = "suburban_root"
  endpoint = var.suburban_proxmox_endpoint
  username = "root@pam"
  password = var.vault_suburban_proxmox_root_password
  insecure = true
}
