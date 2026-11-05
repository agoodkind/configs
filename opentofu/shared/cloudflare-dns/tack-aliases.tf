resource "cloudflare_dns_record" "tack_aliases" {
  provider = cloudflare.dns_tack
  for_each = module.contracts.tack_domain_aliases

  zone_id = each.value
  name    = each.key
  type    = "CNAME"
  content = "${module.contracts.tack_tunnel_id}.cfargotunnel.com"
  ttl     = 1
  proxied = true

  lifecycle {
    prevent_destroy = true
  }
}
