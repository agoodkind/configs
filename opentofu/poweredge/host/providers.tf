provider "proxmox" {
  alias     = "poweredge"
  endpoint  = var.poweredge_proxmox_endpoint
  api_token = "${local.proxmox_token_principal}=${var.vault_poweredge_pve_token_secret}"
  insecure  = true
}

provider "pveguest" {
  nodes = local.nodes

}

terraform {
  required_version = ">= 1.11"
  required_providers {
    http     = { source = "hashicorp/http", version = ">= 3.0" }
    proxmox  = { source = "bpg/proxmox", version = "0.116.0" }
    pveguest = { source = "tofu.home.arpa/agoodkind/pveguest", version = "0.1.0" }
    remote   = { source = "tenstad/remote", version = ">= 0.2.1" }
  }
}
