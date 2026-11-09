variable "vault_cloudflare_tunnel_api_token" {
  description = "Protected Cloudflare tunnel API token for production tunnel identity lookup."
  type        = string
  sensitive   = true
  ephemeral   = true
}
