variable "cloudflare_account_id" {
  description = "Cloudflare account that owns the Zero Trust objects. Stable and not a secret."
  type        = string
  default     = "ee7d7ca7d611ef8c2a07885e8362de0c"
}

variable "cloudflare_mwan_manage_token_file" {
  description = "Protected file containing the separate Cloudflare MWAN load balancer management token"
  type        = string
  default     = "~/.config/mwan/cloudflare-lb-manage.token"
}

variable "vault_tofu_state_passphrase" {
  description = "Passphrase that encrypts the OpenTofu state and plan files"
  type        = string
  sensitive   = true
}
