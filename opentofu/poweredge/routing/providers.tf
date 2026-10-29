provider "pveguest" {
  nodes = local.nodes

}

provider "proxmox" {
  endpoint  = local.nodes["poweredge"].endpoint
  api_token = local.nodes["poweredge"].api_token
  insecure  = local.nodes["poweredge"].insecure
}

terraform {
  required_version = ">= 1.11"
  required_providers {
    mwan     = { source = "tofu.home.arpa/agoodkind/mwan", version = "0.1.0" }
    proxmox  = { source = "bpg/proxmox", version = "0.116.0" }
    pveguest = { source = "tofu.home.arpa/agoodkind/pveguest", version = "0.1.1" }
  }
}
