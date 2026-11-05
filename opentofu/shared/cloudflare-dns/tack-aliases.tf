resource "cloudflare_dns_record" "tack_aliases" {
  provider = cloudflare.dns_tack
  for_each = module.contracts.tack_domain_aliases

  zone_id = each.value
  name    = each.key
  type    = "CNAME"
  content = "4b602332-6413-4f95-8874-561ed6d9b266.cfargotunnel.com"
  ttl     = 1
  proxied = true

  lifecycle {
    prevent_destroy = true
  }
}
