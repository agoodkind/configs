terraform {
  required_providers {
    terrakube = {
      source  = "registry.terraform.io/terrakube-io/terrakube"
      version = "0.34.0"
    }
  }
}

provider "terrakube" {
  endpoint = "https://dashboard.tf.infra.goodkind.io"
  token    = var.vault_terrakube_workspace_api_token
}

locals {
  enrollment = jsondecode(file("${path.root}/../../../.github/terrakube/enrollments.json"))
  deployments = {
    for deployment in flatten([for app in local.enrollment.apps : app.deployments]) :
    trimprefix(deployment.root, "opentofu/") => deployment
  }
}

resource "terrakube_organization_template" "deploy" {
  for_each = local.deployments

  organization_id = var.terrakube_deploy_organization_id
  name            = "${each.value.name}-deploy"
  description     = "Plan and apply a promoted Configs revision with configsctl."
  version         = "1.0.0"
  content = yamlencode({
    flow = [{
      type = "customScripts"
      name = "Deploy Configs"
      step = 100
      commands = [{
        runtime  = "BASH"
        priority = 100
        script   = "exec /usr/local/bin/terrakube-configs ${each.key}"
      }]
    }]
  })

  lifecycle {
    precondition {
      condition = (
        local.enrollment.version == 1 &&
        can(regex("^[a-z0-9]+(-[a-z0-9]+)*$", each.value.name)) &&
        can(regex("^opentofu/[a-z0-9][a-z0-9_-]*(/[a-z0-9][a-z0-9_-]*)+$", each.value.root)) &&
        can(regex("^terrakube/[a-z0-9]+(-[a-z0-9]+)*$", each.value.promotion_branch)) &&
        can(regex("^[a-z0-9]+(-[a-z0-9]+)*$", each.value.environment))
      )
      error_message = "The deployment enrollment requires version 1 and a valid name, Configs root and promotion branch."
    }
  }
}

resource "terrakube_workspace_vcs" "deploy" {
  for_each = local.deployments

  organization_id                     = var.terrakube_deploy_organization_id
  vcs_id                              = var.terrakube_deploy_vcs_id
  name                                = "${each.value.name}-deploy"
  execution_mode                      = "remote"
  repository                          = "https://github.com/agoodkind/configs.git"
  branch                              = each.value.promotion_branch
  folder                              = each.value.root
  template_id                         = terrakube_organization_template.deploy[each.key].id
  iac_type                            = "tofu"
  iac_version                         = "1.13.9"
  allow_remote_apply                  = false
  restrict_apply_to_configured_branch = true
}

resource "terrakube_federated_credential" "deploy_queue" {
  for_each = local.deployments

  name       = "configs-deploy-${each.value.name}"
  issuer_url = "https://token.actions.githubusercontent.com"
  audience   = "terrakube-configs-deploy"
}

resource "terrakube_federated_credential_claim" "deploy_queue" {
  for_each = merge([
    for selector, deployment in local.deployments : {
      for key, value in {
        sub          = "repo:agoodkind/configs:environment:${deployment.environment}"
        ref          = "refs/heads/main"
        workflow_ref = "agoodkind/configs/.github/workflows/terrakube-queue.yml@refs/heads/main"
      } : "${selector}:${key}" => { selector = selector, key = key, value = value }
    }
  ]...)

  federated_credential_id = terrakube_federated_credential.deploy_queue[each.value.selector].id
  claim_key               = each.value.key
  claim_value             = each.value.value
}

resource "terrakube_workspace_access" "deploy_queue" {
  for_each = local.deployments

  organization_id  = var.terrakube_deploy_organization_id
  workspace_id     = terrakube_workspace_vcs.deploy[each.key].id
  name             = terrakube_federated_credential.deploy_queue[each.key].name
  role             = "custom"
  manage_job       = false
  plan_job         = true
  approve_job      = true
  destroy_job      = false
  manage_state     = false
  manage_workspace = false

  depends_on = [terrakube_federated_credential_claim.deploy_queue]
}

resource "terrakube_workspace_variable" "executor_setting" {
  for_each = merge([
    for selector in keys(local.deployments) : {
      for key, value in {
        TERRAKUBE_ENABLE_EPHEMERAL_EXECUTOR = "1"
        EPHEMERAL_CONFIG_ENVFROM_CONFIG_MAP = "terrakube-executor-config,terrakube-executor-connection"
      } : "${selector}:${key}" => { selector = selector, key = key, value = value }
    }
  ]...)

  organization_id = var.terrakube_deploy_organization_id
  workspace_id    = terrakube_workspace_vcs.deploy[each.value.selector].id
  key             = each.value.key
  value           = each.value.value
  category        = "ENV"
  description     = "Configure the dedicated executor job."
  hcl             = false
  sensitive       = false
}

resource "terrakube_workspace_variable" "service_account" {
  for_each = local.deployments

  organization_id  = var.terrakube_deploy_organization_id
  workspace_id     = terrakube_workspace_vcs.deploy[each.key].id
  key              = "OP_SERVICE_ACCOUNT_TOKEN_${upper(replace(each.value.name, "-", "_"))}"
  value_wo         = var.vault_terrakube_service_account_tokens[each.key]
  value_wo_version = var.terrakube_deployment_secret_versions[each.key]
  category         = "ENV"
  description      = "Read the 1Password vault for this deployment."
  hcl              = false
  sensitive        = true
}

output "deployment_targets" {
  value = {
    for selector, workspace in terrakube_workspace_vcs.deploy : selector => {
      organization_id = var.terrakube_deploy_organization_id
      workspace_id    = workspace.id
      template_id     = terrakube_organization_template.deploy[selector].id
      environment     = local.deployments[selector].environment
    }
  }
}
