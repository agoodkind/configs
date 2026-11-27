module "configuration" { source = "../../modules/shared/terrakube-webhook-dns/configuration" }
module "contracts" { source = "../../modules/contracts" }

locals {
  zone_id = module.contracts.cloudflare_zone_ids["goodkind.io"]
}

data "cloudflare_zero_trust_tunnel_cloudflared" "terrakube" {
  provider = cloudflare.tunnels

  account_id = var.cloudflare_account_id
  filter = {
    name       = "vault-terrakube"
    is_deleted = false
  }

  lifecycle {
    postcondition {
      condition     = self.name == "vault-terrakube"
      error_message = "Provision the Terrakube tunnel before publishing the webhook hostname."
    }
  }
}

resource "cloudflare_certificate_pack" "terrakube_webhook" {
  provider = cloudflare.certificates

  zone_id               = local.zone_id
  type                  = "advanced"
  hosts                 = ["goodkind.io", module.configuration.webhook.hostname]
  certificate_authority = "lets_encrypt"
  validation_method     = "txt"
  validity_days         = 90
}

resource "cloudflare_dns_record" "terrakube_webhook" {
  provider = cloudflare.dns

  zone_id = local.zone_id
  name    = module.configuration.webhook.hostname
  type    = "CNAME"
  content = "${data.cloudflare_zero_trust_tunnel_cloudflared.terrakube.id}.cfargotunnel.com"
  ttl     = 1
  proxied = true

  lifecycle {
    precondition {
      condition     = cloudflare_certificate_pack.terrakube_webhook.status == "active"
      error_message = "Wait for Cloudflare to activate the Terrakube webhook certificate before publishing its hostname."
    }
  }
}
