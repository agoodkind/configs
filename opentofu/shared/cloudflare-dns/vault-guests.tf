locals {
  vault_guest_keys = toset([
    "debianct",
    "proxy",
    "seaweedfs",
    "tack",
    "tack_app2",
    "tack_data1",
    "tack_data2",
    "tack_data3",
    "tack_search1",
  ])
}

resource "cloudflare_dns_record" "vault_guests" {
  provider = cloudflare.dns
  for_each = { for key in local.vault_guest_keys : key => local.service_mapping[key] }

  zone_id = local.cloudflare_zone_ids["goodkind.io"]
  name    = each.value.hostname
  type    = "AAAA"
  content = each.value.ipv6
  ttl     = 1
  proxied = false

  lifecycle {
    prevent_destroy = true
  }
}

resource "cloudflare_dns_record" "tack_public" {
  provider = cloudflare.dns

  zone_id = local.cloudflare_zone_ids["goodkind.io"]
  name    = "tack.goodkind.io"
  type    = "CNAME"
  content = "${module.contracts.tack_tunnel_id}.cfargotunnel.com"
  ttl     = 1
  proxied = true

  lifecycle {
    prevent_destroy = true
  }
}
