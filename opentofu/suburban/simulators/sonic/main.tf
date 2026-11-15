module "contracts" { source = "../../../modules/contracts" }
module "hypervisor" { source = "../../../modules/suburban/services/suburban_hypervisor" }
module "configuration" { source = "../../../modules/suburban/simulators/sonic/configuration" }

locals {
  proxmox_token_principal = module.contracts.proxmox_token_principal
  nodes = {
    suburban = {
      endpoint  = "https://[${module.hypervisor.value.ipv6}]:8006"
      api_token = "${local.proxmox_token_principal}=${var.vault_suburban_testbed_pve_token_secret}"
      insecure  = true
      node_name = "hypervisor"
    }
  }
}

# Retain the backend and providers until OpenTofu destroys the simulator guests.
removed {
  from = module.suburban
  lifecycle { destroy = true }
}
