terraform {
  required_version = ">= 1.11"
  required_providers {
    opadmin = {
      source  = "registry.opentofu.org/agoodkind/opadmin"
      version = "0.1.0"
    }
  }
}

provider "opadmin" {}
