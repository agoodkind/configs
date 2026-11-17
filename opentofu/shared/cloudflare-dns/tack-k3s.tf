variable "cloudflare_account_id" {
  description = "Cloudflare account that owns the dedicated production tunnel."
  type        = string
  default     = "ee7d7ca7d611ef8c2a07885e8362de0c"
}

data "cloudflare_zero_trust_tunnel_cloudflared" "tack_k3s" {
  provider = cloudflare.tunnels

  account_id = var.cloudflare_account_id
  filter = {
    name       = module.contracts.tack_k3s_tunnel_names.prod
    is_deleted = false
  }

  lifecycle {
    postcondition {
      condition = (
        self.name == module.contracts.tack_k3s_tunnel_names.prod &&
        self.id != module.contracts.tack_tunnel_id
      )
      error_message = "Provision exactly one active tack-k3s production tunnel distinct from the legacy shared tunnel before changing Tack DNS."
    }
  }
}

locals {
  tack_tunnel_target    = "${data.cloudflare_zero_trust_tunnel_cloudflared.tack_k3s.id}.cfargotunnel.com"
  tack_private_hostname = "tack.home.goodkind.io"
}

resource "cloudflare_certificate_pack" "tack_private" {
  provider              = cloudflare.certificates
  zone_id               = local.cloudflare_zone_ids["goodkind.io"]
  type                  = "advanced"
  hosts                 = ["goodkind.io", local.tack_private_hostname]
  certificate_authority = "lets_encrypt"
  validation_method     = "txt"
  validity_days         = 90
}

resource "cloudflare_dns_record" "tack_private" {
  provider = cloudflare.dns
  for_each = toset([local.tack_private_hostname])

  zone_id = local.cloudflare_zone_ids["goodkind.io"]
  name    = each.key
  type    = "CNAME"
  content = local.tack_tunnel_target
  ttl     = 1
  proxied = true

  lifecycle {
    precondition {
      condition     = cloudflare_certificate_pack.tack_private.status == "active"
      error_message = "Wait for Cloudflare to activate the Tack edge certificate before switching the private hostname."
    }
  }
}
