provider "pveguest" {
  nodes = {
    vault = {
      endpoint  = regex("^https://[^/]+", var.proxmox_endpoint)
      api_token = "${local.proxmox_token_principal}=${var.vault_proxmox_token_secret}"
      insecure  = true
      node_name = local.service_mapping.proxy.node
    }
  }
}

provider "proxmox" {
  endpoint  = var.proxmox_endpoint
  api_token = "${local.proxmox_token_principal}=${var.vault_proxmox_token_secret}"
  insecure  = true
}

terraform {
  required_version = ">= 1.11"
  required_providers {
    http     = { source = "hashicorp/http", version = ">= 3.0" }
    pveguest = { source = "tofu.home.arpa/agoodkind/pveguest", version = "0.1.3" }
    proxmox  = { source = "bpg/proxmox", version = "0.116.0" }
  }
}
