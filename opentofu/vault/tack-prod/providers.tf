provider "proxmox" {
  endpoint  = var.proxmox_endpoint
  api_token = "${local.proxmox_token_principal}=${var.vault_proxmox_token_secret}"
  insecure  = true

  ssh {
    agent    = true
    username = "root"
    node {
      name    = "vault"
      address = local.service_mapping.vault_hypervisor.ipv6
    }
  }
}

provider "pveguest" {
  nodes           = local.nodes
  smtp2go_api_key = var.vault_smtp2go_api_key
}

terraform {
  required_version = ">= 1.11"
  required_providers {
    http     = { source = "hashicorp/http", version = ">= 3.0" }
    proxmox  = { source = "bpg/proxmox", version = "0.116.0" }
    pveguest = { source = "tofu.home.arpa/agoodkind/pveguest", version = "0.1.3" }
  }
}
