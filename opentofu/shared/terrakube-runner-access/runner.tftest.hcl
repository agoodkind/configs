mock_provider "cloudflare" { alias = "tunnels" }
mock_provider "cloudflare" { alias = "tunnel_routes" }
mock_provider "cloudflare" { alias = "zero_trust" }
mock_provider "cloudflare" { alias = "enrollment" }

variables {
  vault_terrakube_runner_service_token_ids = {
    qa      = "11111111-1111-1111-1111-111111111111"
    prod    = "22222222-2222-2222-2222-222222222222"
    preview = "33333333-3333-3333-3333-333333333333"
  }
  vault_cloudflare_terrakube_runner_enrollment_api_token = "disposable-test-input"
  vault_cloudflare_tunnel_routes_api_token               = "disposable-test-input"
  vault_cloudflare_tunnel_api_token                      = "disposable-test-input"
  vault_cloudflare_zero_trust_api_token                  = "disposable-test-input"
  vault_tofu_state_passphrase                            = "disposable-test-input"
}

override_data {
  target = data.cloudflare_zero_trust_tunnel_cloudflared.terrakube
  values = {
    name = "vault-terrakube"
    id   = "44444444-4444-4444-4444-444444444444"
  }
}

override_resource {
  target = cloudflare_zero_trust_tunnel_cloudflared_virtual_network.runner
  values = {
    id = "55555555-5555-5555-5555-555555555555"
  }
}

run "runner_scope" {
  command = apply

  assert {
    condition = alltrue([
      for runner, profile in cloudflare_zero_trust_device_custom_profile.runner :
      profile.match == format("identity.service_token_uuid == %q", var.vault_terrakube_runner_service_token_ids[runner]) &&
      profile.virtual_networks.allowed == tolist(["55555555-5555-5555-5555-555555555555"]) &&
      profile.virtual_networks.default == "55555555-5555-5555-5555-555555555555" &&
      one(profile.include).address == "fd42:7466:3::126/128"
    ])
    error_message = "Each runner must match its service token UUID and select only the dashboard route and runner network."
  }

  assert {
    condition     = cloudflare_zero_trust_gateway_policy.allow_dashboard.traffic == "net.vnet_id == \"55555555-5555-5555-5555-555555555555\" and any(net.dst.ip[*] in {fd42:7466:3::126/128}) and net.dst.port == 443 and net.protocol == \"tcp\""
    error_message = "Runner traffic must allow only TCP 443 to the dashboard private address."
  }

  assert {
    condition = (
      cloudflare_zero_trust_gateway_policy.block_other_traffic.traffic == "net.vnet_id == \"55555555-5555-5555-5555-555555555555\"" &&
      cloudflare_zero_trust_gateway_policy.block_other_traffic.action == "block" &&
      cloudflare_zero_trust_gateway_policy.block_other_traffic.precedence > cloudflare_zero_trust_gateway_policy.allow_dashboard.precedence &&
      cloudflare_zero_trust_tunnel_cloudflared_route.dashboard.virtual_network_id == "55555555-5555-5555-5555-555555555555"
    )
    error_message = "The runner network must block other traffic after the dashboard allow rule."
  }

  assert {
    condition = (
      cloudflare_zero_trust_access_policy.runner_enrollment.decision == "non_identity" &&
      toset([for selector in cloudflare_zero_trust_access_policy.runner_enrollment.include : selector.service_token.token_id]) ==
      toset(values(var.vault_terrakube_runner_service_token_ids))
    )
    error_message = "WARP enrollment must allow exactly the three distinct runner service tokens."
  }
}

run "reject_duplicate_identity" {
  command = plan

  variables {
    vault_terrakube_runner_service_token_ids = {
      qa      = "11111111-1111-1111-1111-111111111111"
      prod    = "11111111-1111-1111-1111-111111111111"
      preview = "33333333-3333-3333-3333-333333333333"
    }
  }

  expect_failures = [var.vault_terrakube_runner_service_token_ids]
}
