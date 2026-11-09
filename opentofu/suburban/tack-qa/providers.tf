provider "proxmox" {
  alias     = "suburban"
  endpoint  = var.suburban_proxmox_endpoint
  api_token = "${local.proxmox_token_principal}=${var.vault_suburban_testbed_pve_token_secret}"
  insecure  = true
}

provider "pveguest" {
  nodes = local.nodes

  smtp2go_api_key = var.vault_smtp2go_api_key
}

provider "docker" {
  alias = "tack_app2_suburban"
  host  = var.docker_hosts["tack_app2_suburban"]
}

terraform {
  required_version = ">= 1.11"
  required_providers {
    docker   = { source = "kreuzwerker/docker", version = "4.6.0" }
    http     = { source = "hashicorp/http", version = ">= 3.0" }
    proxmox  = { source = "bpg/proxmox", version = "0.116.0" }
    pveguest = { source = "tofu.home.arpa/agoodkind/pveguest", version = "0.1.3" }
  }
}
