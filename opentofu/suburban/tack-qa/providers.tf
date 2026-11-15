provider "proxmox" {
  alias     = "suburban"
  endpoint  = var.suburban_proxmox_endpoint
  api_token = "${local.proxmox_token_principal}=${var.vault_suburban_testbed_pve_token_secret}"
  insecure  = true

  ssh {
    agent    = true
    username = "root"
    node {
      name    = "hypervisor"
      address = local.service_mapping.suburban_hypervisor.ipv6
    }
  }
}

provider "pveguest" {
  nodes = local.nodes

}

terraform {
  required_version = ">= 1.11"
  required_providers {
    proxmox  = { source = "bpg/proxmox", version = "0.116.0" }
    pveguest = { source = "tofu.home.arpa/agoodkind/pveguest", version = "0.1.3" }
    remote   = { source = "tenstad/remote", version = ">= 0.2.1" }
  }
}
