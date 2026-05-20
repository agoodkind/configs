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
  }
  required_version = ">= 1.9"
}

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
