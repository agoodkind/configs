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

variable "vault_terrakube_deployment_secrets" {
  type = map(object({
    vault           = string
    vault_password  = string
    ssh_private_key = string
    ssh_known_hosts = string
    provider_token  = string
  }))
  sensitive = true
  ephemeral = true

  description = "Workspace credentials indexed by the Configs workspace selector. Each vault contains only the selected workspace's inputs."

  validation {
    condition     = toset(keys(var.vault_terrakube_deployment_secrets)) == toset(["suburban/tack-qa", "vault/tack-prod"])
    error_message = "Supply credentials for both deployment workspace selectors."
  }
}

variable "terrakube_deployment_secret_versions" {
  type        = map(number)
  description = "Increment the selected workspace version whenever its credentials change."

  validation {
    condition = (
      toset(keys(var.terrakube_deployment_secret_versions)) == toset(["suburban/tack-qa", "vault/tack-prod"]) &&
      alltrue([for revision in values(var.terrakube_deployment_secret_versions) : revision > 0 && floor(revision) == revision])
    )
    error_message = "Supply a positive integer credential version for each deployment workspace."
  }
}
