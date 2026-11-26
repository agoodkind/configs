mock_provider "terrakube" {}

variables {
  vault_tofu_state_passphrase         = "synthetic-state-passphrase"
  vault_terrakube_workspace_api_token = "synthetic-api-token"
  terrakube_deploy_organization_id    = "00000000-0000-0000-0000-000000000001"
  terrakube_deploy_vcs_id             = "00000000-0000-0000-0000-000000000002"
  vault_terrakube_service_account_tokens = {
    "suburban/tack-qa" = "synthetic-qa"
    "vault/tack-prod"  = "synthetic-prod"
  }
  terrakube_deployment_secret_versions = { "suburban/tack-qa" = 1, "vault/tack-prod" = 2 }
}

run "bind_one_service_account_to_each_deployment" {
  command = apply

  assert {
    condition = alltrue([
      for selector, binding in terrakube_workspace_variable.service_account :
      binding.workspace_id == terrakube_workspace_vcs.deploy[selector].id &&
      binding.key == "OP_SERVICE_ACCOUNT_TOKEN_${upper(replace(local.deployments[selector].name, "-", "_"))}" &&
      binding.value_wo_version == var.terrakube_deployment_secret_versions[selector] &&
      binding.sensitive && binding.category == "ENV"
    ]) && length(terrakube_workspace_variable.service_account) == length(local.deployments)
    error_message = "Each deployment workspace must receive its own sensitive service account variable and rotation version."
  }
}

run "reject_shared_service_account" {
  command = plan

  variables {
    vault_terrakube_service_account_tokens = {
      "suburban/tack-qa" = "synthetic-shared"
      "vault/tack-prod"  = "synthetic-shared"
    }
  }

  expect_failures = [var.vault_terrakube_service_account_tokens]
}
