module "terrakube" { source = "../../modules/vault/terrakube-prod/configuration" }

resource "cloudflare_zero_trust_tunnel_cloudflared" "terrakube" {
  provider = cloudflare.tunnels

  account_id = var.cloudflare_account_id
  name       = "vault-terrakube"
  config_src = "cloudflare"

  lifecycle {
    prevent_destroy = true
  }
}

resource "cloudflare_zero_trust_tunnel_cloudflared_config" "terrakube" {
  provider = cloudflare.tunnels

  account_id = var.cloudflare_account_id
  tunnel_id  = cloudflare_zero_trust_tunnel_cloudflared.terrakube.id
  source     = "cloudflare"
  config = {
    ingress = [
      {
        hostname       = module.terrakube.webhook.hostname
        path           = module.terrakube.webhook.path
        service        = module.terrakube.webhook.origin
        origin_request = { http_host_header = module.terrakube.webhook.hostname }
      },
      { service = "http_status:404" },
    ]
    warp_routing = { enabled = true }
  }
}

resource "cloudflare_zero_trust_tunnel_cloudflared_route" "terrakube_dashboard" {
  provider = cloudflare.tunnel_routes

  account_id         = var.cloudflare_account_id
  network            = module.terrakube.dashboard.route
  tunnel_id          = cloudflare_zero_trust_tunnel_cloudflared.terrakube.id
  virtual_network_id = "8a034fb6-d06e-4c08-a567-7860e351635e"
  comment            = "Terrakube dashboard"
}

data "cloudflare_zero_trust_tunnel_cloudflared_token" "terrakube" {
  provider = cloudflare.tunnels

  account_id = var.cloudflare_account_id
  tunnel_id  = cloudflare_zero_trust_tunnel_cloudflared.terrakube.id
}

provider "pveguest" {
  alias = "terrakube"
  nodes = {
    vault = {
      endpoint  = "https://${module.terrakube.service_mapping.vault_hypervisor.hostname}:8006"
      node_name = module.terrakube.guest.node_name
      api_token = "${module.contracts.proxmox_token_principal}=${var.vault_proxmox_token_secret}"
      insecure  = true
    }
  }
}

module "terrakube_cloudflared" {
  source = "../../modules/platform/cloudflared"

  providers = { pveguest = pveguest.terrakube }

  guest            = module.terrakube.guest
  tunnel_id        = cloudflare_zero_trust_tunnel_cloudflared.terrakube.id
  token            = data.cloudflare_zero_trust_tunnel_cloudflared_token.terrakube.token
  token_version    = 1
  connector_count  = 1
  namespace        = module.terrakube.namespace
  connector_prefix = "terrakube-cloudflared"
  manifest_prefix  = "terrakube-cloudflared"
  revision_label   = "terrakube.goodkind.io/connector-revision"
  image            = module.terrakube.cloudflared_image

  depends_on = [cloudflare_zero_trust_tunnel_cloudflared_config.terrakube]
}

resource "cloudflare_zero_trust_gateway_policy" "terrakube_dashboard_dns" {
  provider = cloudflare.zero_trust

  account_id = var.cloudflare_account_id
  name       = "Terrakube dashboard private DNS"
  action     = "override"
  filters    = ["dns"]
  traffic    = "dns.fqdn == \"${module.terrakube.dashboard.hostname}\""
  precedence = 850
  enabled    = true
  rule_settings = {
    override_ips = [module.terrakube.dashboard.ipv6]
  }
}
