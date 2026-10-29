terraform {
  required_providers {
    proxmox = {
      source  = "bpg/proxmox"
      version = "0.116.0"
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
  # The data_wo argument of proxmox_acme_dns_plugin is write-only. Write-only
  # arguments require OpenTofu 1.11.
  required_version = ">= 1.11"
}
