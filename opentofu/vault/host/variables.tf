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

variable "proxmox_endpoint" {
  description = "Proxmox API base URL for the production vault host including port"
  type        = string
  default     = "https://[3d06:bad:b01::254]:8006/"
}

variable "vault_proxmox_acme_cloudflare_token" {
  description = "Cloudflare DNS token that the production vault host uses for ACME DNS challenges"
  type        = string
  sensitive   = true
}

# `./configsctl tofu` sets each variable named vault_* from the Ansible vault key
# with the same name.
variable "vault_proxmox_token_secret" {
  description = "Secret of the Proxmox API token for the production vault host"
  type        = string
  sensitive   = true
}

variable "vault_tofu_state_passphrase" {
  description = "Passphrase that encrypts the OpenTofu state and plan files"
  type        = string
  sensitive   = true
}
