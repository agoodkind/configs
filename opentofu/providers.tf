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
  required_version = ">= 1.9"
}

# Reads CLOUDFLARE_API_TOKEN from the environment. configsctl passes its own
# environment through to the tofu child process, so exporting the token before
# running configsctl is enough until configsctl reads it from the vault itself.
provider "cloudflare" {}

provider "proxmox" {
  endpoint  = var.proxmox_endpoint
  api_token = var.proxmox_api_token
  insecure  = true
}

provider "proxmox" {
  alias     = "suburban"
  endpoint  = var.suburban_proxmox_endpoint
  api_token = var.suburban_proxmox_api_token
  insecure  = true
}

provider "proxmox" {
  alias    = "suburban_root"
  endpoint = var.suburban_proxmox_endpoint
  username = "root@pam"
  password = var.suburban_proxmox_root_password
  insecure = true
}
