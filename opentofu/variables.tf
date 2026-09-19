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

variable "cloudflare_account_id" {
  description = "Cloudflare account that owns the Zero Trust objects. Stable and not a secret."
  type        = string
  default     = "ee7d7ca7d611ef8c2a07885e8362de0c"
}

variable "cloudflare_owner_email" {
  description = "Identity the Cloudflare device profiles match on"
  type        = string
  default     = "alex@goodkind.io"
}

variable "berylax_beacon_sockaddr" {
  description = "Host and port of the TLS endpoint proving a device is on the Berylax LAN"
  type        = string
  default     = "10.230.255.254:443"
}

variable "berylax_beacon_sha256" {
  description = "SHA-256 fingerprint of the certificate served at berylax_beacon_sockaddr"
  type        = string
  default     = "23a17d915ce0e1fa3eb5b1271c136c2bd3e00bd5d4d9cf079b60a99a956024fc"
}
