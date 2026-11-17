module "contracts" {
  source = "../../modules/contracts"
}

locals {
  service_mapping = module.configuration.service_mapping
}

module "vault" {
  source = "../../modules/vault/debianct"

  providers = { proxmox = proxmox }

  service_mapping = local.service_mapping
}


locals { proxmox_token_principal = module.contracts.proxmox_token_principal }

module "configuration" { source = "../../modules/vault/debianct/configuration" }
