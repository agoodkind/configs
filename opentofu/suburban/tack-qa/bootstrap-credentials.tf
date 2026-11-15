variable "vault_tack_qa_app_password" {
  description = "Protected QA app password from the encrypted credential source."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tack_qa_yugabyte_password" {
  description = "Protected QA yugabyte password from the encrypted credential source."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tack_qa_migrator_password" {
  description = "Protected QA migrator password from the encrypted credential source."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tack_qa_audit_operator_password" {
  description = "Protected QA audit operator password from the encrypted credential source."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tack_qa_audit_reader_password" {
  description = "Protected QA audit reader password from the encrypted credential source."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tack_qa_audit_redactor_password" {
  description = "Protected QA audit redactor password from the encrypted credential source."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tack_qa_audit_writer_password" {
  description = "Protected QA audit writer password from the encrypted credential source."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tack_qa_seed_api_token" {
  description = "Protected QA seed api token from the encrypted credential source."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tack_qa_audit_signing_key" {
  description = "Protected QA audit signing key from the encrypted credential source."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tack_qa_ledger_server_certificate" {
  description = "Protected QA ledger server certificate from the encrypted credential source."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tack_qa_ledger_server_private_key" {
  description = "Protected QA ledger server private key from the encrypted credential source."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tack_qa_search_admin_certificate" {
  description = "Protected QA search admin certificate from the encrypted credential source."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tack_qa_search_admin_private_key" {
  description = "Protected QA search admin private key from the encrypted credential source."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tack_qa_search_password_hash" {
  description = "Protected QA search password hash from the encrypted credential source."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tack_search1_suburban_certificate" {
  description = "Protected QA OpenSearch node certificate."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tack_search1_suburban_private_key" {
  description = "Protected QA OpenSearch node private key."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "tack_qa_signing_id" {
  description = "Public signer identifier matching the protected QA audit signing key."
  type        = string
  default     = "ed25519:a90f49b639e4335f"
}

variable "tack_qa_search_admin_dn" {
  description = "Subject DN of the QA OpenSearch admin certificate."
  type        = string
  default     = "CN=tack-search-qa-admin"
}

variable "tack_qa_search_node_dn" {
  description = "Subject DN of the QA OpenSearch node certificate."
  type        = string
  default     = "CN=tack-search1.suburban.goodkind.io"
}
