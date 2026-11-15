variable "suburban_proxmox_endpoint" {
  description = "Proxmox API base URL for the suburban testbed host including port"
  type        = string
  default     = "https://[3d06:bad:b01:200::1]:8006/"
}

variable "vault_smtp2go_api_key" {
  description = "SMTP2GO API key for guest relay deletion and the retained host credential."
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

variable "host_artifact" {
  description = "Select the verified signed artifact for Suburban host configuration."
  type = object({
    release_tag     = string
    asset           = string
    sha256          = string
    bundle_asset    = string
    bundle_sha256   = string
    source_revision = string
    program_bundles = map(object({
      asset  = string
      sha256 = string
    }))
  })
}

variable "host_smtp2go_secret_version" {
  description = "Increase this version when the host SMTP2GO secret changes."
  type        = number

  validation {
    condition     = var.host_smtp2go_secret_version >= 1 && floor(var.host_smtp2go_secret_version) == var.host_smtp2go_secret_version
    error_message = "The host SMTP2GO secret version must be a positive integer."
  }
}

variable "vault_tofu_state_passphrase" {
  description = "Passphrase that encrypts the OpenTofu state and plan files"
  type        = string
  sensitive   = true
  ephemeral   = true
}
