variable "vault_cloudflare_goodkind_io_waf_api_token" {
  type      = string
  sensitive = true
  ephemeral = true
}

variable "vault_tofu_state_passphrase" {
  type      = string
  sensitive = true
}
