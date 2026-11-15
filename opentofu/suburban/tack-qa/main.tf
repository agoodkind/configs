module "contracts" { source = "../../modules/contracts" }
module "hypervisor" { source = "../../modules/suburban/services/suburban_hypervisor" }
locals {
  proxmox_token_principal = module.contracts.proxmox_token_principal
  service_mapping         = { suburban_hypervisor = module.hypervisor.value }
  nodes = { suburban = {
    endpoint  = "https://[${module.hypervisor.value.ipv6}]:8006"
    api_token = "${local.proxmox_token_principal}=${var.vault_suburban_testbed_pve_token_secret}"
    insecure  = true
    node_name = "hypervisor"
  } }
}
