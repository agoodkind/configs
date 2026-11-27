locals {
  vault_guest_keys = toset([
    "seaweedfs",
    "tack",
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
}

resource "cloudflare_dns_record" "tack_public" {
  provider = cloudflare.dns

  zone_id = local.cloudflare_zone_ids["goodkind.io"]
  name    = "tack.goodkind.io"
  type    = "CNAME"
  content = local.tack_tunnel_target
  ttl     = 1
  proxied = true

  lifecycle {
    prevent_destroy = true
  }
}
