provider "proxmox" {
  alias     = "suburban"
  endpoint  = var.suburban_proxmox_endpoint
  api_token = "${local.proxmox_token_principal}=${var.vault_suburban_testbed_pve_token_secret}"
  insecure  = true
}

terraform {
  required_version = ">= 1.11"
  required_providers {
    http    = { source = "hashicorp/http", version = ">= 3.0" }
    proxmox = { source = "bpg/proxmox", version = "0.116.0" }
    remote  = { source = "tenstad/remote", version = ">= 0.2.1" }
  }
}
