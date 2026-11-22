moved {
  from = cloudflare_dns_record.static["goodkind.io/A/unifi.home.goodkind.io/e1a87412"]
  to   = cloudflare_dns_record.retired_unifi_ipv4
}

moved {
  from = cloudflare_dns_record.static["goodkind.io/AAAA/unifi.home.goodkind.io/3f88d2f2"]
  to   = cloudflare_dns_record.retired_unifi_ipv6
}
