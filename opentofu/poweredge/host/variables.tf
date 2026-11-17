variable "force_acme_certificate_renewal" {
  description = "Set this value to true for an apply that must replace an existing certificate."
  type        = bool
  default     = false
}

variable "cloudflare_account_id" {
  description = "Cloudflare account that owns the Zero Trust objects. Stable and not a secret."
  type        = string
  default     = "ee7d7ca7d611ef8c2a07885e8362de0c"
}

variable "cloudflare_owner_email" {
  description = "Identity the Cloudflare device profiles match on"
  type        = string
  default     = "alex@goodkind.io"
}

variable "vault_poweredge_acme_cloudflare_token" {
  description = "Cloudflare DNS token that the poweredge host uses for ACME DNS challenges"
  type        = string
  sensitive   = true
}

variable "vault_poweredge_pve_token_secret" {
  description = "Secret of the Proxmox API token for the poweredge host"
  type        = string
  sensitive   = true
}

variable "vault_smtp2go_api_key" {
  type      = string
  sensitive = true
}

variable "vault_tofu_state_passphrase" {
  description = "Passphrase that encrypts the OpenTofu state and plan files"
  type        = string
  sensitive   = true
}
