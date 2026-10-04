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
  # proxmox_acme_dns_plugin uses a write-only argument, which needs OpenTofu 1.11.
  required_version = ">= 1.11"
}

# The provider reads CLOUDFLARE_API_TOKEN from the environment inherited by configsctl.
provider "cloudflare" {}

provider "cloudflare" {
  alias     = "mwan_manage"
  api_token = sensitive(trimspace(file(pathexpand(var.cloudflare_mwan_manage_token_file))))
}

locals {
  # Both hypervisors issue the automation token under the principal that the
  # shared Ansible variables define.
  proxmox_token_principal = "${local.shared_vars.proxmox_api_user}!${local.shared_vars.proxmox_token_id}"
}

provider "proxmox" {
  endpoint  = var.proxmox_endpoint
  api_token = "${local.proxmox_token_principal}=${var.vault_proxmox_token_secret}"
  insecure  = true
}

provider "proxmox" {
  alias    = "poweredge_root"
  endpoint = var.poweredge_proxmox_endpoint
  username = "root@pam"
  password = var.vault_poweredge_proxmox_root_password
  insecure = true
}

module "vault_root_login" {
  source = "./root_login"

  endpoint = var.proxmox_endpoint
  password = var.vault_proxmox_root_password
  otp      = var.proxmox_root_otp
}

provider "proxmox" {
  alias                 = "vault_root"
  endpoint              = var.proxmox_endpoint
  auth_ticket           = module.vault_root_login.auth_ticket
  csrf_prevention_token = module.vault_root_login.csrf_prevention_token
  insecure              = true
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
