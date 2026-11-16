variable "cloudflare_account_id" {
  type    = string
  default = "ee7d7ca7d611ef8c2a07885e8362de0c"
}

variable "vault_cloudflare_api_token" {
  type      = string
  sensitive = true
  ephemeral = true
}

variable "vault_cloudflare_pages_api_token" {
  type      = string
  sensitive = true
  ephemeral = true
}

variable "vault_tofu_state_passphrase" {
  type      = string
  sensitive = true
}
