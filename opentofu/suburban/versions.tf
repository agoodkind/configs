terraform {
  required_providers {
    proxmox = {
      source                = "bpg/proxmox"
      version               = ">= 0.106.0"
      configuration_aliases = [proxmox.root]
    }
  }
}
