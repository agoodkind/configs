variable "vault_smtp2go_api_key" {
  description = "SMTP2GO API key for host mail"
  type        = string
  sensitive   = true
}

variable "vault_tofu_state_passphrase" {
  description = "Passphrase that encrypts the OpenTofu state and plan files"
  type        = string
  sensitive   = true
}
