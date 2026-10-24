variable "vault_cloudflare_api_token" {
  description = "Cloudflare DNS API token supplied by the Ansible vault."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_cloudflare_silver_flare_dns_api_token" {
  description = "Cloudflare DNS API token for silver-flare.com supplied by the Ansible vault."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tofu_state_passphrase" {
  description = "Passphrase that encrypts the OpenTofu state and plan files"
  type        = string
  sensitive   = true
}
