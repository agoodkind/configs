locals {
  tack_connectors = {
    for environment, connector in module.configuration.tack_connectors : environment => connector
    if contains(var.tack_k3s_environments, environment)
  }
  tack_proxmox_credentials = {
    prod = var.vault_proxmox_token_secret
    qa   = var.vault_suburban_testbed_pve_token_secret
  }
  tack_ingress = {
    prod = concat([
      for hostname in concat(["tack.goodkind.io", "tack.home.goodkind.io"], sort(keys(module.contracts.tack_domain_aliases))) : {
        hostname       = hostname
        service        = module.configuration.tack_application_identities.prod.origin
        origin_request = { http_host_header = "tack.home.goodkind.io" }
      }
    ], [{ service = "http_status:404" }])
    qa = [{ service = module.configuration.tack_application_identities.qa.origin }]
  }
}

data "cloudflare_zero_trust_tunnel_cloudflared" "production" {
  provider   = cloudflare.tunnels
  count      = contains(var.tack_k3s_environments, "prod") ? 0 : 1
  account_id = var.cloudflare_account_id
  filter = {
    name       = module.contracts.tack_k3s_tunnel_names.prod
    is_deleted = false
  }
}

resource "cloudflare_zero_trust_tunnel_cloudflared" "tack_k3s" {
  provider = cloudflare.tunnels
  for_each = local.tack_connectors

  account_id = var.cloudflare_account_id
  name       = module.contracts.tack_k3s_tunnel_names[each.key]
  config_src = "cloudflare"

  lifecycle {
    prevent_destroy = true
  }
}

resource "cloudflare_zero_trust_tunnel_cloudflared_config" "tack_k3s" {
  provider = cloudflare.tunnels
  for_each = local.tack_connectors

  account_id = var.cloudflare_account_id
  tunnel_id  = cloudflare_zero_trust_tunnel_cloudflared.tack_k3s[each.key].id
  source     = "cloudflare"
  config = {
    ingress      = local.tack_ingress[each.key]
    warp_routing = each.key == "prod" ? { enabled = true } : null
  }
}

data "cloudflare_zero_trust_tunnel_cloudflared_token" "tack_k3s" {
  provider = cloudflare.tunnels
  for_each = local.tack_connectors

  account_id = var.cloudflare_account_id
  tunnel_id  = cloudflare_zero_trust_tunnel_cloudflared.tack_k3s[each.key].id
}

module "tack_cloudflared" {
  source   = "../../modules/platform/cloudflared"
  for_each = local.tack_connectors

  guest            = each.value.guest
  tunnel_id        = cloudflare_zero_trust_tunnel_cloudflared.tack_k3s[each.key].id
  token            = data.cloudflare_zero_trust_tunnel_cloudflared_token.tack_k3s[each.key].token
  token_version    = each.value.token_version
  connector_count  = each.value.connector_count
  namespace        = module.configuration.tack_application_identities[each.key].namespace
  connector_prefix = module.configuration.tack_application_identities[each.key].connector_prefix
  manifest_prefix  = module.configuration.tack_application_identities[each.key].connector_manifest_prefix
  revision_label   = module.configuration.tack_application_identities[each.key].revision_label
  image            = module.configuration.cloudflared_image

  depends_on = [cloudflare_zero_trust_tunnel_cloudflared_config.tack_k3s]
}
