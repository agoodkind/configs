module "contracts" { source = "../../../modules/contracts" }
module "hypervisor" { source = "../../../modules/configuration/services/suburban_hypervisor" }
module "configuration" { source = "../../../modules/suburban/simulators/mbrains/configuration" }

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

removed {
  from = module.suburban
  lifecycle { destroy = true }
}
