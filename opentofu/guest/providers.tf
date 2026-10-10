terraform {
  required_providers {
    # OpenTofu installs pveguest from this workspace's terraform.d/plugins.
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
