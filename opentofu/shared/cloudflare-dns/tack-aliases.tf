resource "cloudflare_dns_record" "tack_aliases" {
  provider = cloudflare.dns_tack
  for_each = module.contracts.tack_domain_aliases

  zone_id = each.value
  name    = each.key
  type    = "CNAME"
  content = local.tack_tunnel_target
  ttl     = 1
  proxied = true

  lifecycle {
    prevent_destroy = true
  }
}
