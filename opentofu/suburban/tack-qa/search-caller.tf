module "search_caller" {
  source = "../../modules/tack-k3s/search-caller"

  host   = local.service_mapping.suburban_hypervisor.ipv6
  caller = module.configuration.tack_k3s.guest.ipv6
}
