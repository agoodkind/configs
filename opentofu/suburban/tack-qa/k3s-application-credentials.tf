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
