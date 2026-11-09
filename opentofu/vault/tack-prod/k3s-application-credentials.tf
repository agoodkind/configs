variable "vault_tack_k3s_database_url" {
  description = "Production application database URL from the encrypted vault."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tack_k3s_audit_operator_dsn" {
  description = "Production application audit operator DSN from the encrypted vault."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tack_k3s_fdb_bootstrap" {
  description = "Initial production FoundationDB descriptor from the encrypted vault."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tack_search_password" {
  description = "Production search runtime password from the encrypted vault."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tack_search_cursor_key" {
  description = "Production search cursor key from the encrypted vault."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tack_search_ca" {
  description = "Production search CA certificate from the encrypted vault."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tack_ghcr_pull_token" {
  description = "Production GHCR pull token from the encrypted vault."
  type        = string
  sensitive   = true
  ephemeral   = true
}
