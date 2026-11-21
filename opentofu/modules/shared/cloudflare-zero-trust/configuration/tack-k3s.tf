module "tack_prod" { source = "../../../configuration/tack/prod" }
module "tack_qa" { source = "../../../configuration/tack/qa" }

locals {
  tack_connectors = {
    prod = {
      connector_count = module.tack_prod.connector_count
      guest           = module.tack_prod.tack_k3s.guest
      token_version   = 1
      hypervisor = {
        address   = module.tack_prod.service_mapping.vault_hypervisor.hostname
        node_name = module.tack_prod.tack_k3s.guest.node_name
      }
    }
    qa = {
      connector_count = module.tack_qa.connector_count
      guest           = module.tack_qa.tack_k3s.guest
      token_version   = 1
      hypervisor = {
        address   = module.tack_qa.service_mapping.suburban_hypervisor.ipv6
        node_name = module.tack_qa.tack_k3s.guest.node_name
      }
    }
  }
}

output "tack_connectors" { value = local.tack_connectors }

output "tack_application_identities" {
  value = {
    prod = module.tack_prod.application_identity
    qa   = module.tack_qa.application_identity
  }
}

output "cloudflared_image" { value = module.tack_prod.cloudflared_image }
