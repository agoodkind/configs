module "contracts" {
  source = "../../modules/contracts"
}

locals {
  zone     = "goodkind.io"
  hostname = "apt.goodkind.io"
}

resource "cloudflare_pages_project" "send_email_apt" {
  provider = cloudflare.pages

  account_id        = var.cloudflare_account_id
  name              = "send-email-apt"
  production_branch = "main"
}

resource "cloudflare_pages_domain" "send_email_apt" {
  provider = cloudflare.pages

  account_id   = var.cloudflare_account_id
  project_name = cloudflare_pages_project.send_email_apt.name
  name         = local.hostname
}

resource "cloudflare_dns_record" "send_email_apt" {
  provider = cloudflare.dns

  zone_id = module.contracts.cloudflare_zone_ids[local.zone]
  name    = local.hostname
  type    = "CNAME"
  content = cloudflare_pages_project.send_email_apt.subdomain
  ttl     = 1
  proxied = true
}

module "configuration" { source = "../../modules/shared/send-email-apt/configuration" }
