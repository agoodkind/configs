terraform {
  required_providers {
    # OpenTofu installs this provider from the implied local mirror in
    # ~/.terraform.d/plugins. No registry serves tofu.home.arpa.
    pveguest = {
      source  = "tofu.home.arpa/agoodkind/pveguest"
      version = "0.1.0"
    }
    http = {
      source  = "hashicorp/http"
      version = ">= 3.0"
    }
  }
  required_version = ">= 1.11"
}

provider "pveguest" {
  nodes = local.nodes
}
