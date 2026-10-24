locals { repository_root = abspath("${path.module}/../../..") }

module "contracts" {
  source          = "../../modules/contracts"
  repository_root = local.repository_root
}

locals {
  shared_vars         = module.contracts.shared_vars
  service_mapping     = module.contracts.service_mapping
  cloudflare_zone_ids = module.contracts.cloudflare_zone_ids
}

module "poweredge" {
  source = "../../modules/firmware/poweredge"

  nic_functions = {
    "NIC.Embedded.1-1-1" = "NIC.Embedded.1"
    "NIC.Embedded.2-1-1" = "NIC.Embedded.1"
    "NIC.Embedded.3-1-1" = "NIC.Embedded.1"
    "NIC.Embedded.4-1-1" = "NIC.Embedded.1"
  }
  maintenance_window = var.poweredge_maintenance_window
}

output "observed_nic_attributes" {
  description = "Independent iDRAC NIC attribute readback. The provider resource does not refresh configured OEM attribute values."
  value       = module.poweredge.observed_nic_attributes
}
