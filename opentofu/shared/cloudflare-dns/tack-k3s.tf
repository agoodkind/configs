variable "tack_hostname_cutover" {
  description = "Desired hostname stage: legacy, public, aliases, or private. Each stage includes the preceding hostname changes."
  type        = string
  default     = "private"

  validation {
    condition     = contains(["legacy", "public", "aliases", "private"], var.tack_hostname_cutover)
    error_message = "Set tack_hostname_cutover to legacy, public, aliases, or private."
  }
}

variable "cloudflare_account_id" {
  description = "Cloudflare account that owns the dedicated production tunnel."
  type        = string
  default     = "ee7d7ca7d611ef8c2a07885e8362de0c"
}

data "cloudflare_zero_trust_tunnel_cloudflared" "tack_k3s" {
  provider = cloudflare.tunnels
  for_each = var.tack_hostname_cutover == "legacy" ? toset([]) : toset(["prod"])

  account_id = var.cloudflare_account_id
  filter = {
    name       = module.contracts.tack_k3s_tunnel_names[each.key]
    is_deleted = false
  }

  lifecycle {
    postcondition {
      condition = (
        self.name == module.contracts.tack_k3s_tunnel_names[each.key] &&
        self.id != module.contracts.tack_tunnel_id
      )
      error_message = "Provision exactly one active tack-k3s production tunnel distinct from the legacy shared tunnel before changing Tack DNS."
    }
  }
}

locals {
  tack_public_tunnel_id = var.tack_hostname_cutover == "legacy" ? module.contracts.tack_tunnel_id : data.cloudflare_zero_trust_tunnel_cloudflared.tack_k3s["prod"].id
  tack_alias_tunnel_id  = contains(["aliases", "private"], var.tack_hostname_cutover) ? data.cloudflare_zero_trust_tunnel_cloudflared.tack_k3s["prod"].id : module.contracts.tack_tunnel_id
}

resource "cloudflare_dns_record" "tack_private" {
  provider = cloudflare.dns
  for_each = var.tack_hostname_cutover == "private" ? toset(["tack.home.goodkind.io"]) : toset([])

  zone_id = local.cloudflare_zone_ids["goodkind.io"]
  name    = each.key
  type    = "CNAME"
  content = "${data.cloudflare_zero_trust_tunnel_cloudflared.tack_k3s["prod"].id}.cfargotunnel.com"
  ttl     = 1
  proxied = true
}
