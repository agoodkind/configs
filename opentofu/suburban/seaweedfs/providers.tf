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
