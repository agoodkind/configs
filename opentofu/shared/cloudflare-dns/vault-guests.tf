locals {
  vault_guest_keys = toset([
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

moved {
  from = cloudflare_dns_record.vault_guests["debianct"]
  to   = cloudflare_dns_record.retired_debianct
}

moved {
  from = cloudflare_dns_record.vault_guests["proxy"]
  to   = cloudflare_dns_record.retired_proxy
}

moved {
  from = cloudflare_dns_record.static["goodkind.io/A/*.home.goodkind.io/f078473b"]
  to   = cloudflare_dns_record.retired_proxy_wildcard_ipv4
}

moved {
  from = cloudflare_dns_record.static["goodkind.io/AAAA/*.home.goodkind.io/9b9c74a5"]
  to   = cloudflare_dns_record.retired_proxy_wildcard_ipv6
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
