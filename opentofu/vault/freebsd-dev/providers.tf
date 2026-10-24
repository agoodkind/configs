provider "proxmox" {
  endpoint  = var.proxmox_endpoint
  api_token = "${local.proxmox_token_principal}=${var.vault_proxmox_token_secret}"
  insecure  = true
}

terraform {
  required_version = ">= 1.11"
  required_providers {
    http    = { source = "hashicorp/http", version = ">= 3.0" }
    proxmox = { source = "bpg/proxmox", version = "0.116.0" }
  }
}
