variable "vault_tofu_state_passphrase" {
  type      = string
  sensitive = true
}

variable "vault_terrakube_workspace_api_token" {
  type      = string
  sensitive = true
  ephemeral = true
}

variable "terrakube_deploy_organization_id" {
  type        = string
  description = "ID of the Terrakube organization reserved for automated Configs deploy jobs."
}

variable "terrakube_deploy_vcs_id" {
  type        = string
  description = "ID of the read-only Configs GitHub connection in the deployment organization."
}

variable "vault_terrakube_service_account_tokens" {
  type        = map(string)
  sensitive   = true
  ephemeral   = true
  description = "Read-only 1Password service account token for each enrolled Configs workspace selector."

  validation {
    condition = (
      toset(keys(var.vault_terrakube_service_account_tokens)) == toset(keys(local.deployments)) &&
      alltrue([for token in values(var.vault_terrakube_service_account_tokens) : try(length(trimspace(token)) > 0, false)]) &&
      length(distinct(values(var.vault_terrakube_service_account_tokens))) == length(local.deployments)
    )
    error_message = "Supply a different nonempty service account token for each enrolled deployment."
  }
}

variable "terrakube_deployment_secret_versions" {
  type        = map(number)
  description = "Increment the selected workspace version whenever its credentials change."

  validation {
    condition = (
      toset(keys(var.terrakube_deployment_secret_versions)) == toset(keys(local.deployments)) &&
      alltrue([for revision in values(var.terrakube_deployment_secret_versions) : revision > 0 && floor(revision) == revision])
    )
    error_message = "Supply a positive integer credential version for each deployment workspace."
  }
}
