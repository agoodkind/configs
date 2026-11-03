locals { repository_root = abspath("${path.module}/../../..") }

module "contracts" {
  source          = "../../modules/contracts"
  repository_root = local.repository_root
}

locals { proxmox_token_principal = module.contracts.proxmox_token_principal }

removed {
  from = module.vault.proxmox_virtual_environment_container.sit6
  lifecycle { destroy = true }
}

# Container deletion removes these files and their systemd unit.
removed {
  from = pveguest_file.retirement
  lifecycle { destroy = false }
}

removed {
  from = pveguest_systemd_unit.routing_retired
  lifecycle { destroy = false }
}
