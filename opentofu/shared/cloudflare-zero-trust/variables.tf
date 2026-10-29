variable "berylax_beacon_sha256" {
  description = "SHA-256 fingerprint of the certificate served at berylax_beacon_sockaddr"
  type        = string
  default     = "23a17d915ce0e1fa3eb5b1271c136c2bd3e00bd5d4d9cf079b60a99a956024fc"
}

variable "berylax_beacon_sockaddr" {
  description = "Host and port of the TLS endpoint proving a device is on the Berylax LAN"
  type        = string
  default     = "10.230.255.254:443"
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

variable "cloudflare_router_routes_enabled" {
  description = "Create the WireGuard client routes after the home-router connector reports healthy."
  type        = bool
  default     = true
}

variable "poweredge_beacon" {
  description = "TLS endpoint and public certificate fingerprint for the PowerEdge LAN beacon."
  type = object({
    tls_sockaddr = string
    sha256       = string
  })
  default = {
    tls_sockaddr = "10.230.255.253:443"
    sha256       = "24d5ca5094d5fb28ae2b340d107e3c2f26f12631c81d30b5f6e81976292ad70d"
  }
}

variable "vault_cloudflare_tunnel_api_token" {
  description = "Cloudflare API token that manages cloudflared tunnels in the homelab account."
  type        = string
  sensitive   = true
}

variable "vault_cloudflare_tunnel_routes_api_token" {
  description = "The Ansible vault supplies the Cloudflare private routes API token."
  type        = string
  sensitive   = true
}

variable "vault_cloudflare_zero_trust_api_token" {
  description = "The Ansible vault supplies the Cloudflare Zero Trust API token."
  type        = string
  sensitive   = true
}

variable "vault_tofu_state_passphrase" {
  description = "Passphrase that encrypts the OpenTofu state and plan files"
  type        = string
  sensitive   = true
}
