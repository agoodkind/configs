terraform {
  required_version = ">= 1.11"
  required_providers {
    http     = { source = "hashicorp/http", version = ">= 3.0" }
    proxmox  = { source = "bpg/proxmox", version = "0.116.0" }
    pveguest = { source = "tofu.home.arpa/agoodkind/pveguest", version = "0.1.9" }
  }
}

provider "proxmox" {
  endpoint  = var.proxmox_endpoint
  api_token = local.api_token
  insecure  = true

  ssh {
    agent    = true
    username = "root"
    node {
      name    = local.guest.node_name
      address = module.configuration.service_mapping.vault_hypervisor.hostname
    }
  }
}

provider "pveguest" {
  nodes = {
    (local.guest.node) = {
      endpoint  = trimsuffix(var.proxmox_endpoint, "/")
      api_token = local.api_token
      insecure  = true
      node_name = local.guest.node_name
    }
  }
}
