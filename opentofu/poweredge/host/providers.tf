provider "proxmox" {
  alias     = "poweredge"
  endpoint  = "https://[${local.host.ipv6}]:8006/"
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
    pveguest = { source = "tofu.home.arpa/agoodkind/pveguest", version = "0.1.4" }
    remote   = { source = "tenstad/remote", version = ">= 0.2.1" }
    linux    = { source = "labbs/linux", version = "1.3.0" }
  }
}
