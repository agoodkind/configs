variable "suburban_proxmox_endpoint" {
  description = "Proxmox API base URL for the suburban testbed host including port"
  type        = string
  default     = "https://[3d06:bad:b01:200::1]:8006/"
}

variable "vault_smtp2go_api_key" {
  description = "The pveguest provider authenticates to SMTP2GO with this ephemeral, sensitive API key. ./configsctl tofu guest reads the value from the vault key vault_smtp2go_api_key."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_suburban_testbed_pve_token_secret" {
  description = "Secret of the Proxmox API token for the suburban testbed host"
  type        = string
  sensitive   = true
}

variable "vault_tofu_state_passphrase" {
  description = "Passphrase that encrypts the OpenTofu state and plan files"
  type        = string
  sensitive   = true
}
