# Each ACME account and DNS plugin depends on module.overlay. The automation
# token of the provider needs the Sys.ACME roles that module.overlay grants.
#
# `tofu test` fails on an import block. Import an existing account with id
# "default", a plugin with id "cf", and a certificate with its node name
# through `./configsctl tofu import`.
locals {
  acme_directory = "https://acme-v02.api.letsencrypt.org/directory"
  acme_terms     = "https://letsencrypt.org/documents/LE-SA-v1.5-February-24-2025.pdf"
  acme_account   = "default"
  acme_plugin    = "cf"

  # OpenTofu sends data_wo to Proxmox again only when this number increases.
  # Increase it after a change of a Cloudflare DNS token.
  acme_plugin_data_version = 1
}

resource "proxmox_acme_account" "vault" {
  name      = local.acme_account
  contact   = var.cloudflare_owner_email
  directory = local.acme_directory
  tos       = local.acme_terms

  depends_on = [module.overlay]
}

resource "proxmox_acme_dns_plugin" "vault" {
  plugin = local.acme_plugin
  api    = "cf"

  data_wo = {
    CF_Account_ID = var.cloudflare_account_id
    CF_Zone_ID    = local.cloudflare_zone_ids["goodkind.io"]
    CF_Token      = var.vault_proxmox_acme_cloudflare_token
  }
  data_wo_version = local.acme_plugin_data_version

  depends_on = [module.overlay]
}

resource "proxmox_acme_certificate" "vault" {
  node_name = "vault"
  account   = proxmox_acme_account.vault.name

  domains = [
    {
      domain = "vault.home.goodkind.io"
      plugin = proxmox_acme_dns_plugin.vault.plugin
    }
  ]
}

resource "proxmox_acme_account" "suburban" {
  provider  = proxmox.suburban
  name      = local.acme_account
  contact   = var.cloudflare_owner_email
  directory = local.acme_directory
  tos       = local.acme_terms

  depends_on = [module.overlay]
}

resource "proxmox_acme_dns_plugin" "suburban" {
  provider = proxmox.suburban
  plugin   = local.acme_plugin
  api      = "cf"

  data_wo = {
    CF_Account_ID = var.cloudflare_account_id
    CF_Zone_ID    = local.cloudflare_zone_ids["goodkind.io"]
    CF_Token      = var.vault_suburban_acme_cloudflare_token
  }
  data_wo_version = local.acme_plugin_data_version

  depends_on = [module.overlay]
}

resource "proxmox_acme_certificate" "suburban" {
  provider  = proxmox.suburban
  node_name = "hypervisor"
  account   = proxmox_acme_account.suburban.name

  domains = [
    {
      domain = "hypervisor.suburban.goodkind.io"
      plugin = proxmox_acme_dns_plugin.suburban.plugin
    }
  ]
}

resource "proxmox_acme_account" "poweredge" {
  provider  = proxmox.poweredge
  name      = local.acme_account
  contact   = var.cloudflare_owner_email
  directory = local.acme_directory
  tos       = local.acme_terms

  depends_on = [module.overlay]
}

resource "proxmox_acme_dns_plugin" "poweredge" {
  provider = proxmox.poweredge
  plugin   = local.acme_plugin
  api      = "cf"

  data_wo = {
    CF_Account_ID = var.cloudflare_account_id
    CF_Zone_ID    = local.cloudflare_zone_ids["goodkind.io"]
    CF_Token      = var.vault_poweredge_acme_cloudflare_token
  }
  data_wo_version = local.acme_plugin_data_version

  depends_on = [module.overlay]
}

resource "proxmox_acme_certificate" "poweredge" {
  provider  = proxmox.poweredge
  node_name = "poweredge"
  account   = proxmox_acme_account.poweredge.name

  domains = [
    {
      domain = "poweredge.home.goodkind.io"
      plugin = proxmox_acme_dns_plugin.poweredge.plugin
    }
  ]
}
