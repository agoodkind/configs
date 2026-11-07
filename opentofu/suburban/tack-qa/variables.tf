variable "suburban_proxmox_endpoint" {
  description = "Proxmox API base URL for the suburban testbed host including port"
  type        = string
  default     = "https://[3d06:bad:b01:200::1]:8006/"
}

variable "vault_smtp2go_api_key" {
  description = "SMTP2GO API key for the pveguest provider"
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_suburban_testbed_pve_token_secret" {
  description = "Secret of the Proxmox API token for the suburban testbed host"
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "docker_hosts" {
  description = "Docker Engine unix:// socket address for each Docker provider alias. configsctl starts a bridge for each docker_bridges entry and sets this value. Operators must not set it."
  type        = map(string)
  ephemeral   = true
}

variable "vault_tofu_state_passphrase" {
  description = "Passphrase that encrypts the OpenTofu state and plan files"
  type        = string
  sensitive   = true
}
