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
    null = {
      source  = "hashicorp/null"
      version = ">= 3.0"
    }
  }
  required_version = ">= 1.9"
}

# Default provider targets the production vault Proxmox host.
# Existing LXC resources in containers.tf use this provider implicitly.
provider "proxmox" {
  endpoint  = var.proxmox_endpoint
  api_token = var.proxmox_api_token
  insecure  = true
}

# Suburban provider alias targets the suburban testbed Proxmox host.
# All MWAN testbed resources (bridges, VM 950) use this alias explicitly.
provider "proxmox" {
  alias     = "suburban"
  endpoint  = var.suburban_proxmox_endpoint
  api_token = var.suburban_proxmox_api_token
  insecure  = true
}
