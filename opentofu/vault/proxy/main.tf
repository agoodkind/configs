module "contracts" {
  source = "../../modules/contracts"
}

removed {
  from = module.vault

  lifecycle {
    destroy = true
  }
}

locals { proxmox_token_principal = module.contracts.proxmox_token_principal }

module "configuration" { source = "../../modules/vault/proxy/configuration" }
