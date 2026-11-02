locals { repository_root = abspath("${path.module}/../../..") }

module "contracts" {
  source          = "../../modules/contracts"
  repository_root = local.repository_root
}

# Retire the production router from its existing VM resource address.
removed {
  from = module.vault.proxmox_virtual_environment_vm.opnsense

  lifecycle {
    destroy = true
  }
}

locals { proxmox_token_principal = module.contracts.proxmox_token_principal }
