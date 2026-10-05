# `./configsctl tofu` sets each variable named vault_* from the Ansible vault key
# with the same name.
variable "vault_proxmox_token_secret" {
  description = "Secret of the Proxmox API token for the production vault host"
  type        = string
  sensitive   = true
}

variable "vault_suburban_testbed_pve_token_secret" {
  description = "Secret of the Proxmox API token for the suburban testbed host"
  type        = string
  sensitive   = true
}

variable "vault_poweredge_pve_token_secret" {
  description = "Secret of the Proxmox API token for the poweredge host"
  type        = string
  sensitive   = true
}
