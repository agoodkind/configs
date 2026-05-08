variable "proxmox_api_token" {
  description = "Proxmox API token for the production vault host in the form user@pam!tokenid=secret"
  type        = string
  sensitive   = true
}

variable "proxmox_endpoint" {
  description = "Proxmox API base URL for the production vault host including port"
  type        = string
  default     = "https://[3d06:bad:b01::254]:8006/"
}

variable "suburban_proxmox_api_token" {
  description = "Proxmox API token for the suburban testbed host in the form user@pam!tokenid=secret"
  type        = string
  sensitive   = true
}

variable "suburban_proxmox_endpoint" {
  description = "Proxmox API base URL for the suburban testbed host including port"
  type        = string
  default     = "https://[3d06:bad:b01:200::1]:8006/"
}

variable "github_ssh_keys_user" {
  description = "GitHub username whose public keys are injected into new containers"
  type        = string
  default     = "agoodkind"
}
