variable "vault_tack_qa_k3s_database_url" {
  description = "QA application database URL from the encrypted vault."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tack_qa_k3s_audit_operator_dsn" {
  description = "QA application audit operator DSN from the encrypted vault."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tack_qa_k3s_fdb_bootstrap" {
  description = "Initial QA FoundationDB descriptor from the encrypted vault."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tack_qa_search_password" {
  description = "QA search runtime password from the encrypted vault."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tack_qa_search_cursor_key" {
  description = "QA search cursor key from the encrypted vault."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tack_qa_search_ca" {
  description = "QA search CA certificate from the encrypted vault."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tack_qa_ghcr_pull_token" {
  description = "QA GHCR pull token from the encrypted vault."
  type        = string
  sensitive   = true
  ephemeral   = true
}
