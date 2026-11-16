module "search_caller" {
  source = "../../modules/tack/search-caller"

  host   = local.service_mapping.vault_hypervisor.ipv6
  caller = module.configuration.tack_k3s.guest.ipv6
}
