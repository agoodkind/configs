variable "proxmox_api_token" {
  description = "Proxmox API token in the form user@pam!tokenid=secret"
  type        = string
  sensitive   = true
}

variable "proxmox_endpoint" {
  description = "Proxmox API base URL including port"
  type        = string
  default     = "https://[3d06:bad:b01::254]:8006/"
}

variable "github_ssh_keys_user" {
  description = "GitHub username whose public keys are injected into new containers"
  type        = string
  default     = "agoodkind"
}
