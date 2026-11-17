module "contracts" {
  source = "../../modules/contracts"
}

locals {
  zone_id = module.contracts.cloudflare_zone_ids["goodkind.io"]

  bot_fight_mode = ["http_request_sbfm"]
  bot_fight_mode_and_managed_rules = [
    "http_request_sbfm",
    "http_request_firewall_managed",
  ]
  update_feed_products = ["securityLevel", "bic", "waf", "rateLimit"]
}

import {
  to = cloudflare_ruleset.goodkind_io_custom_firewall
  id = "zones/${local.zone_id}/2a726804d74e4d198e1b5490cd00b3e4"
}

resource "cloudflare_ruleset" "goodkind_io_custom_firewall" {
  zone_id = local.zone_id
  kind    = "zone"
  name    = "default"
  phase   = "http_request_firewall_custom"

  rules = [
    {
      ref         = "6cf67a4eebbd4ec6aee95c24733f9eec"
      enabled     = true
      description = "cloudflared-opnsense-pkg.goodkind.io"
      expression  = "(http.host wildcard r\"cloudflared-opnsense-pkg.goodkind.io\")"
      action      = "skip"
      action_parameters = {
        phases = local.bot_fight_mode
      }
      logging = { enabled = true }
    },
    {
      ref         = "21716d7a7ed84b21918a887ab9f21326"
      enabled     = true
      description = "mdm"
      expression  = "(http.host eq \"mdm.goodkind.io\")"
      action      = "skip"
      action_parameters = {
        phases = local.bot_fight_mode_and_managed_rules
      }
      logging = { enabled = true }
    },
    {
      ref         = "4c0a5b2d122e4bf3b2e34cef749ddbce"
      enabled     = true
      description = "plane"
      expression  = "(http.host eq \"plane.goodkind.io\" and starts_with(http.request.uri.path, \"/api/\"))"
      action      = "skip"
      action_parameters = {
        phases = local.bot_fight_mode
      }
      logging = { enabled = true }
    },
    {
      ref         = "c24142224f3f43518995b09f6cc7a613"
      enabled     = true
      description = "huly"
      expression  = "(http.host eq \"huly.goodkind.io\")"
      action      = "skip"
      action_parameters = {
        phases = local.bot_fight_mode
      }
      logging = { enabled = true }
    },
    {
      ref         = "2e282671aeab49958f69071f789a27c1"
      enabled     = true
      description = "go-get vanity imports"
      expression  = "(http.host eq \"goodkind.io\" and http.request.uri.query contains \"go-get=1\")"
      action      = "skip"
      action_parameters = {
        phases = local.bot_fight_mode
      }
      logging = { enabled = true }
    },
    {
      ref         = "56d68036ca1849049715b8adf6f4c467"
      enabled     = true
      description = "Sparkle appcast and update feed"
      expression  = "(http.host eq \"goodkind.io\" and (starts_with(http.request.uri.path, \"/fancurve\") or starts_with(http.request.uri.path, \"/stickies-improved\")))"
      action      = "skip"
      action_parameters = {
        phases   = local.bot_fight_mode_and_managed_rules
        products = local.update_feed_products
      }
      logging = { enabled = true }
    },
    {
      ref         = "064e360257bc46dfb65d4da3fe99f439"
      enabled     = true
      description = "PlainStickies appcast and update feed"
      expression  = "(http.host eq \"goodkind.io\" and starts_with(http.request.uri.path, \"/plainstickies\"))"
      action      = "skip"
      action_parameters = {
        phases   = local.bot_fight_mode_and_managed_rules
        products = local.update_feed_products
      }
      logging = { enabled = true }
    },
    {
      ref         = "b771985e28db4334a3a771d28a2b69e8"
      enabled     = true
      description = "Clyde OpenAI API"
      expression  = "(http.host eq \"clyde-suburban.goodkind.io\")"
      action      = "skip"
      action_parameters = {
        phases   = local.bot_fight_mode_and_managed_rules
        products = local.update_feed_products
      }
      logging = { enabled = true }
    },
    {
      enabled     = true
      description = "apt.goodkind.io"
      expression  = "(http.host eq \"apt.goodkind.io\")"
      action      = "skip"
      action_parameters = {
        phases = local.bot_fight_mode
      }
      logging = { enabled = true }
    },
  ]

  lifecycle {
    prevent_destroy = true
  }
}

module "configuration" { source = "../../modules/shared/cloudflare-waf/configuration" }
