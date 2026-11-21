provider "proxmox" {
  endpoint  = var.proxmox_endpoint
  api_token = "${local.proxmox_token_principal}=${var.vault_proxmox_token_secret}"
  insecure  = true
}

terraform {
  required_version = ">= 1.11"
  required_providers {
    pveguest = { source = "tofu.home.arpa/agoodkind/pveguest", version = "0.1.8" }
    http     = { source = "hashicorp/http", version = ">= 3.0" }
    proxmox  = { source = "bpg/proxmox", version = "0.116.0" }
    remote   = { source = "tenstad/remote", version = ">= 0.2.1" }
    linux    = { source = "labbs/linux", version = "1.3.0" }
  }
}

provider "pveguest" {
  nodes = { vault = {
    endpoint  = trimsuffix(var.proxmox_endpoint, "/")
    api_token = "${local.proxmox_token_principal}=${var.vault_proxmox_token_secret}"
    insecure  = true
  } }
}
