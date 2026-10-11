terraform {
  required_providers {
    # OpenTofu installs pveguest from this workspace's terraform.d/plugins.
    pveguest = {
      source  = "tofu.home.arpa/agoodkind/pveguest"
      version = "0.1.0"
    }
    proxmox = {
      source  = "bpg/proxmox"
      version = "0.116.0"
    }
    http = {
      source  = "hashicorp/http"
      version = ">= 3.0"
    }
  }
  required_version = ">= 1.11"
}

provider "pveguest" {
  nodes = local.nodes
}

provider "proxmox" {
  endpoint  = local.nodes["poweredge"].endpoint
  api_token = local.nodes["poweredge"].api_token
  insecure  = local.nodes["poweredge"].insecure
}
