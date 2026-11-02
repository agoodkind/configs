locals { repository_root = abspath("${path.module}/../../..") }

module "contracts" {
  source          = "../../modules/contracts"
  repository_root = local.repository_root
}

# Retain CT 112 and its disk when removing service ownership from state.
removed {
  from = module.vault.proxmox_virtual_environment_container.adguard

  lifecycle {
    destroy = false
  }
}

locals { proxmox_token_principal = module.contracts.proxmox_token_principal }
