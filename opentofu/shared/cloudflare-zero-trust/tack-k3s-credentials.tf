variable "vault_proxmox_token_secret" {
  description = "Protected Proxmox API token secret for production connector guest files."
  type        = string
  sensitive   = true
  ephemeral   = true
}
