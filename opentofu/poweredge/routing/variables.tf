variable "vault_poweredge_pve_token_secret" {
  description = "Secret of the Proxmox API token for the poweredge host"
  type        = string
  sensitive   = true
}

variable "vault_tofu_state_passphrase" {
  description = "Passphrase that encrypts the OpenTofu state and plan files"
  type        = string
  sensitive   = true
}
