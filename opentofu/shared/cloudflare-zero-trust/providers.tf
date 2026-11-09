provider "cloudflare" {
  alias     = "tunnel_routes"
  api_token = var.vault_cloudflare_tunnel_routes_api_token
}

provider "cloudflare" {
  alias     = "tunnels"
  api_token = var.vault_cloudflare_tunnel_api_token
}

provider "cloudflare" {
  alias     = "zero_trust"
  api_token = var.vault_cloudflare_zero_trust_api_token
}

provider "pveguest" {
  nodes = {
    for environment, connector in local.tack_connectors : connector.guest.node => {
      endpoint  = "https://[${connector.hypervisor.address}]:8006"
      node_name = connector.hypervisor.node_name
      api_token = "${module.contracts.proxmox_token_principal}=${local.tack_proxmox_credentials[environment]}"
      insecure  = true
    }
  }
}

terraform {
  required_version = ">= 1.11"
  required_providers {
    cloudflare = { source = "cloudflare/cloudflare", version = ">= 5.0.0" }
    pveguest   = { source = "tofu.home.arpa/agoodkind/pveguest", version = "0.1.3" }
  }
}
