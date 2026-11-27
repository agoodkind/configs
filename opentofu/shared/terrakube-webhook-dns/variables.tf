variable "cloudflare_account_id" {
  description = "Cloudflare account that owns the Terrakube tunnel."
  type        = string
  default     = "ee7d7ca7d611ef8c2a07885e8362de0c"
}

variable "vault_cloudflare_goodkind_io_certificates_api_token" {
  description = "Cloudflare token for edge certificates in the goodkind.io zone."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_cloudflare_api_token" {
  description = "Cloudflare DNS API token."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_cloudflare_tunnel_api_token" {
  description = "Cloudflare token for reading the Terrakube tunnel."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tofu_state_passphrase" {
  description = "Passphrase for the encrypted OpenTofu state and plans."
  type        = string
  sensitive   = true
}
