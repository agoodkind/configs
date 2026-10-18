terraform {
  required_version = ">= 1.11"

  required_providers {
    redfish = {
      source  = "registry.terraform.io/dell/redfish"
      version = "1.6.2"
    }
  }
}

provider "redfish" {
  redfish_servers = {
    poweredge = {
      endpoint     = var.poweredge_idrac_endpoint
      user         = var.poweredge_idrac_user
      password     = var.vault_poweredge_idrac_password
      ssl_insecure = var.poweredge_idrac_insecure
    }
  }
}
