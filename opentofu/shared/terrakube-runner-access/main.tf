module "configuration" { source = "../../modules/shared/terrakube-runner-access/configuration" }

locals {
  tunnel_name = "vault-terrakube"
  runners = {
    qa      = { name = "Tack QA", precedence = 470 }
    prod    = { name = "Tack production", precedence = 471 }
    preview = { name = "Tack PR preview", precedence = 472 }
  }
  runner_network  = cloudflare_zero_trust_tunnel_cloudflared_virtual_network.runner.id
  gateway_network = format("net.vnet_id == %q", local.runner_network)
  dashboard_traffic = format(
    "any(net.dst.ip[*] in {%s}) and net.dst.port == 443 and net.protocol == %q",
    module.configuration.dashboard.route,
    "tcp",
  )
}

data "cloudflare_zero_trust_tunnel_cloudflared" "terrakube" {
  provider = cloudflare.tunnels

  account_id = var.cloudflare_account_id
  filter = {
    name       = local.tunnel_name
    is_deleted = false
  }

  lifecycle {
    postcondition {
      condition     = self.name == local.tunnel_name
      error_message = "Provision the Terrakube tunnel before configuring private runner access."
    }
  }
}

resource "cloudflare_zero_trust_tunnel_cloudflared_virtual_network" "runner" {
  provider = cloudflare.tunnel_routes

  account_id         = var.cloudflare_account_id
  name               = "Terrakube GitHub runners"
  is_default_network = false
}

resource "cloudflare_zero_trust_gateway_policy" "allow_dashboard" {
  provider = cloudflare.zero_trust

  account_id = var.cloudflare_account_id
  name       = "Terrakube runners dashboard HTTPS"
  action     = "allow"
  filters    = ["l4"]
  precedence = 100
  enabled    = true
  traffic    = "${local.gateway_network} and ${local.dashboard_traffic}"
}

resource "cloudflare_zero_trust_gateway_policy" "block_other_traffic" {
  provider = cloudflare.zero_trust

  account_id = var.cloudflare_account_id
  name       = "Terrakube runners other destinations"
  action     = "block"
  filters    = ["l4"]
  precedence = 101
  enabled    = true
  traffic    = local.gateway_network
}

resource "cloudflare_zero_trust_tunnel_cloudflared_route" "dashboard" {
  provider = cloudflare.tunnel_routes

  account_id         = var.cloudflare_account_id
  network            = module.configuration.dashboard.route
  tunnel_id          = data.cloudflare_zero_trust_tunnel_cloudflared.terrakube.id
  virtual_network_id = local.runner_network
  comment            = "Terrakube dashboard for GitHub runners"

  depends_on = [
    cloudflare_zero_trust_gateway_policy.allow_dashboard,
    cloudflare_zero_trust_gateway_policy.block_other_traffic,
  ]
}

resource "cloudflare_zero_trust_device_custom_profile" "runner" {
  provider = cloudflare.zero_trust
  for_each = local.runners

  account_id        = var.cloudflare_account_id
  name              = "Terrakube GitHub runner ${each.value.name}"
  match             = format("identity.service_token_uuid == %q", var.vault_terrakube_runner_service_token_ids[each.key])
  precedence        = each.value.precedence
  profile_type      = "warp"
  enabled           = true
  allow_mode_switch = false
  allowed_to_leave  = false
  switch_locked     = true
  service_mode_v2   = { mode = "warp" }
  tunnel_protocol   = "masque"
  include = [{
    address     = module.configuration.dashboard.route
    description = "Terrakube dashboard"
  }]
  virtual_networks = {
    allowed = [local.runner_network]
    default = local.runner_network
  }

  depends_on = [cloudflare_zero_trust_tunnel_cloudflared_route.dashboard]
}

resource "cloudflare_zero_trust_access_policy" "runner_enrollment" {
  provider = cloudflare.enrollment

  account_id = var.cloudflare_account_id
  name       = "Enroll Terrakube GitHub runners"
  decision   = "non_identity"
  include = [
    for runner in keys(local.runners) : {
      service_token = { token_id = var.vault_terrakube_runner_service_token_ids[runner] }
    }
  ]

  depends_on = [cloudflare_zero_trust_device_custom_profile.runner]
}

output "runner_enrollment_policy_id" {
  description = "Policy to add to the existing Cloudflare One Client enrollment application."
  value       = cloudflare_zero_trust_access_policy.runner_enrollment.id
}
