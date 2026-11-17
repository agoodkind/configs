terraform {
  required_version = ">= 1.11"
  required_providers {
    proxmox = { source = "bpg/proxmox", version = "0.116.0" }
  }
}

variable "vault_tofu_state_passphrase" {
  type      = string
  sensitive = true
}

provider "proxmox" {
  username = "root@pam"
}

module "contracts" {
  source = "../../modules/contracts"
}

module "configuration" {
  source = "../../modules/poweredge/permissions/configuration"
}

resource "proxmox_acl" "kernel_modules" {
  path      = "/nodes/poweredge"
  user_id   = module.contracts.shared_vars.proxmox_api_user
  role_id   = "ScopedKernelModules"
  propagate = true
}
