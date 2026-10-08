# `./configsctl tofu` sets each variable named vault_* from the Ansible vault key
# with the same name.
variable "vault_proxmox_token_secret" {
  description = "Secret of the Proxmox API token for the production vault host"
  type        = string
  sensitive   = true
}

variable "vault_proxmox_acme_cloudflare_token" {
  description = "Cloudflare DNS token that the production vault host uses for ACME DNS challenges"
  type        = string
  sensitive   = true
}

variable "proxmox_endpoint" {
  description = "Proxmox API base URL for the production vault host including port"
  type        = string
  default     = "https://[3d06:bad:b01::254]:8006/"
}

variable "vault_suburban_testbed_pve_token_secret" {
  description = "Secret of the Proxmox API token for the suburban testbed host"
  type        = string
  sensitive   = true
}

variable "vault_suburban_proxmox_root_password" {
  description = "Root password for privileged container feature changes on the suburban testbed"
  type        = string
  sensitive   = true
}

variable "vault_suburban_acme_cloudflare_token" {
  description = "Cloudflare DNS token that the suburban testbed host uses for ACME DNS challenges"
  type        = string
  sensitive   = true
}

variable "suburban_proxmox_endpoint" {
  description = "Proxmox API base URL for the suburban testbed host including port"
  type        = string
  default     = "https://[3d06:bad:b01:200::1]:8006/"
}

variable "vault_poweredge_pve_token_secret" {
  description = "Secret of the Proxmox API token for the poweredge host"
  type        = string
  sensitive   = true
}

variable "vault_poweredge_acme_cloudflare_token" {
  description = "Cloudflare DNS token that the poweredge host uses for ACME DNS challenges"
  type        = string
  sensitive   = true
}

variable "poweredge_proxmox_endpoint" {
  description = "Proxmox API base URL for the poweredge host including port"
  type        = string
  default     = "https://poweredge.home.goodkind.io:8006/"
}

variable "vault_tofu_state_passphrase" {
  description = "Passphrase that encrypts the OpenTofu state and plan files"
  type        = string
  sensitive   = true
}

variable "vault_cloudflare_zero_trust_api_token" {
  description = "Cloudflare Zero Trust API token supplied by the Ansible vault"
  type        = string
  sensitive   = true
}

variable "vault_cloudflare_tunnel_routes_api_token" {
  description = "Cloudflare private routes API token supplied by the Ansible vault"
  type        = string
  sensitive   = true
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

variable "cloudflare_mwan_manage_token_file" {
  description = "Protected file containing the separate Cloudflare MWAN load balancer management token"
  type        = string
  default     = "~/.config/mwan/cloudflare-lb-manage.token"
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
