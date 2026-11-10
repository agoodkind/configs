module "tack_prod" { source = "../../../vault/tack-prod/configuration" }
module "tack_qa" { source = "../../../suburban/tack-qa/configuration" }

locals {
  tack_connectors = {
    prod = {
      guest         = module.tack_prod.tack_k3s.guest
      token_version = 1
      hypervisor = {
        address   = module.tack_prod.service_mapping.vault_hypervisor.ipv6
        node_name = module.tack_prod.tack_k3s.guest.node_name
      }
    }
    qa = {
      guest         = module.tack_qa.tack_k3s.guest
      token_version = 1
      hypervisor = {
        address   = module.tack_qa.service_mapping.suburban_hypervisor.ipv6
        node_name = module.tack_qa.tack_k3s.guest.node_name
      }
    }
  }
}

output "tack_connectors" { value = local.tack_connectors }
