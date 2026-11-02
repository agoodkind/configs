variable "suburban_proxmox_endpoint" {
  description = "Proxmox API base URL for the suburban testbed host including port"
  type        = string
  default     = "https://[3d06:bad:b01:200::1]:8006/"
}

variable "vault_suburban_testbed_pve_token_secret" {
  description = "Secret of the Proxmox API token for the suburban testbed host"
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_smtp2go_api_key" {
  description = "SMTP2GO API key that the gateway runtime configuration contains"
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_suburban_testbed_opnsense_api_key" {
  description = "OPNsense API key that the gateway runtime configuration contains"
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_suburban_testbed_opnsense_api_secret" {
  description = "OPNsense API secret that the gateway runtime configuration contains"
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "gateway_secret_serial" {
  description = "Operator serial for the gateway runtime configuration secrets. Increase it after a secret changes. A new serial writes the file again."
  type        = number
  default     = 1
}

variable "vault_tofu_state_passphrase" {
  description = "Passphrase that encrypts the OpenTofu state and plan files"
  type        = string
  sensitive   = true
  ephemeral   = true
}
