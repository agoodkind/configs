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
