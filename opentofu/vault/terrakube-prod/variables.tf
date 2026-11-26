variable "proxmox_endpoint" {
  type    = string
  default = "https://hypervisor.home.goodkind.io:8006/"
}

variable "vault_proxmox_token_secret" {
  type      = string
  sensitive = true
}

variable "vault_tofu_state_passphrase" {
  type      = string
  sensitive = true
}

variable "terrakube_artifact_storage" {
  description = "Dedicated S3-compatible storage for Terrakube artifacts."
  type        = object({ bucket = string, region = string, endpoint = string })

  validation {
    condition = (
      length(trimspace(var.terrakube_artifact_storage.bucket)) > 0 &&
      var.terrakube_artifact_storage.bucket != "tofu-state" &&
      length(trimspace(var.terrakube_artifact_storage.region)) > 0 &&
      startswith(var.terrakube_artifact_storage.endpoint, "https://")
    )
    error_message = "Supply a dedicated Terrakube artifact bucket, region, and HTTPS endpoint."
  }
}

variable "vault_terrakube_k3s_server_token" {
  type      = string
  sensitive = true
  ephemeral = true
}

variable "vault_terrakube_credentials" {
  description = "Credentials for the Terrakube release, local login, PostgreSQL, and artifact storage."
  type = object({
    login_email         = string
    login_password_hash = string
    ldap_bind_password  = string
    postgres_password   = string
    valkey_password     = string
    pat_secret          = string
    internal_secret     = string
    s3_access_key       = string
    s3_secret_key       = string
    dashboard_cert_pem  = string
    dashboard_key_pem   = string
  })
  sensitive = true
  ephemeral = true

  validation {
    condition = (
      can(regex("^[^[:space:]@]+@[^[:space:]@]+$", var.vault_terrakube_credentials.login_email)) &&
      startswith(var.vault_terrakube_credentials.login_password_hash, "{SSHA}") &&
      length(var.vault_terrakube_credentials.ldap_bind_password) >= 32 &&
      length(var.vault_terrakube_credentials.postgres_password) >= 32 &&
      length(var.vault_terrakube_credentials.valkey_password) >= 32 &&
      length(var.vault_terrakube_credentials.pat_secret) >= 32 &&
      length(var.vault_terrakube_credentials.internal_secret) >= 32 &&
      length(var.vault_terrakube_credentials.s3_access_key) > 0 &&
      length(var.vault_terrakube_credentials.s3_secret_key) > 0 &&
      startswith(var.vault_terrakube_credentials.dashboard_cert_pem, "-----BEGIN CERTIFICATE-----") &&
      can(regex("^-----BEGIN (RSA |EC )?PRIVATE KEY-----", var.vault_terrakube_credentials.dashboard_key_pem))
    )
    error_message = "Supply a local login email and SSHA password hash, deployment credentials, and the dashboard TLS certificate and private key."
  }
}

variable "terrakube_credentials_version" {
  description = "Increment this value when any Terrakube deployment credential changes."
  type        = number
  default     = 1

  validation {
    condition     = var.terrakube_credentials_version >= 1 && floor(var.terrakube_credentials_version) == var.terrakube_credentials_version
    error_message = "Use a positive integer credential version."
  }
}
