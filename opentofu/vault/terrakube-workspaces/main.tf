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
  deployments = {
    "suburban/tack-qa" = {
      name   = "tack-qa-deploy"
      branch = "terrakube/tack-qa"
    }
    "vault/tack-prod" = {
      name   = "tack-prod-deploy"
      branch = "terrakube/tack-prod"
    }
  }

  secret_fields = ["vault", "vault_password", "ssh_private_key", "ssh_known_hosts", "provider_token"]

  bindings = merge([
    for selector in keys(local.deployments) : {
      for field in local.secret_fields : "${selector}:${field}" => {
        selector = selector
        field    = field
      }
    }
  ]...)
}

resource "terrakube_organization_template" "deploy" {
  for_each = local.deployments

  organization_id = var.terrakube_deploy_organization_id
  name            = each.value.name
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
}

resource "terrakube_workspace_vcs" "deploy" {
  for_each = local.deployments

  organization_id                     = var.terrakube_deploy_organization_id
  vcs_id                              = var.terrakube_deploy_vcs_id
  name                                = each.value.name
  execution_mode                      = "remote"
  repository                          = "https://github.com/agoodkind/configs.git"
  branch                              = each.value.branch
  folder                              = "opentofu/${each.key}"
  template_id                         = terrakube_organization_template.deploy[each.key].id
  iac_type                            = "tofu"
  iac_version                         = "1.13.9"
  allow_remote_apply                  = false
  restrict_apply_to_configured_branch = true
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

resource "terrakube_workspace_variable" "credential" {
  for_each = local.bindings

  organization_id  = var.terrakube_deploy_organization_id
  workspace_id     = terrakube_workspace_vcs.deploy[each.value.selector].id
  key              = "CONFIGS_DEPLOY_${upper(each.value.field)}"
  value_wo         = var.vault_terrakube_deployment_secrets[each.value.selector][each.value.field]
  value_wo_version = var.terrakube_deployment_secret_versions[each.value.selector]
  category         = "ENV"
  description      = "Protected input for this Configs deployment workspace."
  hcl              = false
  sensitive        = true
}

output "deployment_targets" {
  value = {
    for selector, workspace in terrakube_workspace_vcs.deploy : selector => {
      organization_id = var.terrakube_deploy_organization_id
      workspace_id    = workspace.id
      template_id     = terrakube_organization_template.deploy[selector].id
    }
  }
}
