variable "cloudflare_account_id" {
  description = "Cloudflare account that owns the Terrakube tunnel and WARP organization."
  type        = string
  default     = "ee7d7ca7d611ef8c2a07885e8362de0c"
}

variable "vault_terrakube_runner_service_token_ids" {
  description = "Distinct Cloudflare Access service token resource UUIDs for the QA, production, and PR preview GitHub environments."
  type        = object({ qa = string, prod = string, preview = string })
  nullable    = false
  sensitive   = true

  validation {
    condition = (
      length(distinct(values(var.vault_terrakube_runner_service_token_ids))) == 3 &&
      alltrue([
        for token_id in values(var.vault_terrakube_runner_service_token_ids) :
        can(regex("^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$", token_id))
      ])
    )
    error_message = "Supply three distinct Cloudflare service token resource UUIDs for QA, production, and PR preview."
  }
}

variable "vault_cloudflare_terrakube_runner_enrollment_api_token" {
  description = "Cloudflare token with Access Apps and Policies Write for WARP enrollment."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_cloudflare_tunnel_routes_api_token" {
  description = "Cloudflare token for the Terrakube runner virtual network and route."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_cloudflare_tunnel_api_token" {
  description = "Cloudflare token for reading the Terrakube tunnel."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_cloudflare_zero_trust_api_token" {
  description = "Cloudflare token for WARP profiles and Gateway network policies."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tofu_state_passphrase" {
  description = "Passphrase for the encrypted OpenTofu state and plans."
  type        = string
  sensitive   = true
}
