terraform {
  required_providers {
    proxmox = {
      source                = "bpg/proxmox"
      version               = ">= 0.106.0"
      configuration_aliases = [proxmox.root]
    }
    http = {
      source  = "hashicorp/http"
      version = ">= 3.0"
    }
    # Declares files on the hypervisor over SSH. The registry has no signing
    # key for this provider.
    remote = {
      source  = "tenstad/remote"
      version = ">= 0.2.1"
    }
  }
}
