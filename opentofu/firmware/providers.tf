terraform {
  required_version = ">= 1.11"

  required_providers {
    redfish = {
      source  = "registry.terraform.io/dell/redfish"
      version = "1.6.2"
    }
    restful = {
      source  = "registry.terraform.io/magodo/restful"
      version = "0.25.2"
    }
  }
}
