# ACME certificates for the Proxmox web interface of each hypervisor. Proxmox
# restricts ACME account changes to root@pam. Every resource here uses the
# root provider alias of its hypervisor. Each hypervisor has its own Cloudflare
# DNS token. A root@pam login with a second factor also passes a one-time
# code, which configsctl computes from a seed in the vault.
locals {
  acme_directory = "https://acme-v02.api.letsencrypt.org/directory"
  acme_terms     = "https://letsencrypt.org/documents/LE-SA-v1.5-February-24-2025.pdf"
  acme_account   = "default"
  acme_plugin    = "cf"

  # The plugin data is write-only: OpenTofu does not store a token in state.
  # A rotated token needs a higher version number to be sent again.
  acme_plugin_data_version = 1
}

# Production vault host. The account, the plugin, and the certificate existed
# before OpenTofu managed them. Import ids: account "default", plugin "cf",
# certificate "vault".
resource "proxmox_acme_account" "vault" {
  provider  = proxmox.vault_root
  name      = local.acme_account
  contact   = var.cloudflare_owner_email
  directory = local.acme_directory
  tos       = local.acme_terms
}

resource "proxmox_acme_dns_plugin" "vault" {
  provider = proxmox.vault_root
  plugin   = local.acme_plugin
  api      = "cf"

  data_wo = {
    CF_Account_ID = var.cloudflare_account_id
    CF_Zone_ID    = local.cloudflare_zone_ids["goodkind.io"]
    CF_Token      = var.vault_proxmox_acme_cloudflare_token
  }
  data_wo_version = local.acme_plugin_data_version
}

resource "proxmox_acme_certificate" "vault" {
  provider  = proxmox.vault_root
  node_name = "vault"
  account   = proxmox_acme_account.vault.name

  domains = [
    {
      domain = "vault.home.goodkind.io"
      plugin = proxmox_acme_dns_plugin.vault.plugin
    }
  ]
}

# Suburban testbed host. The account and the plugin existed before OpenTofu
# managed them. The host had no ACME certificate. `tofu test` crashes on import
# blocks. Import the account with id "default" and the plugin with id "cf"
# through `configsctl tofu import`.
#
# The account, the plugin, and the certificate use the automation token, not
# root@pam. The roles ScopedAcmeAccount, ScopedAcmePlugin, and
# ScopedAcmeCertificate in suburban/proxmox_overlays.tf grant the token one
# Sys.ACME privilege for each operation.
resource "proxmox_acme_account" "suburban" {
  provider  = proxmox.suburban
  name      = local.acme_account
  contact   = var.cloudflare_owner_email
  directory = local.acme_directory
  tos       = local.acme_terms
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

# Poweredge host. It had no ACME setup.
resource "proxmox_acme_account" "poweredge" {
  provider  = proxmox.poweredge_root
  name      = local.acme_account
  contact   = var.cloudflare_owner_email
  directory = local.acme_directory
  tos       = local.acme_terms
}

resource "proxmox_acme_dns_plugin" "poweredge" {
  provider = proxmox.poweredge_root
  plugin   = local.acme_plugin
  api      = "cf"

  data_wo = {
    CF_Account_ID = var.cloudflare_account_id
    CF_Zone_ID    = local.cloudflare_zone_ids["goodkind.io"]
    CF_Token      = var.vault_poweredge_acme_cloudflare_token
  }
  data_wo_version = local.acme_plugin_data_version
}

resource "proxmox_acme_certificate" "poweredge" {
  provider  = proxmox.poweredge_root
  node_name = "poweredge"
  account   = proxmox_acme_account.poweredge.name

  domains = [
    {
      domain = "poweredge.home.goodkind.io"
      plugin = proxmox_acme_dns_plugin.poweredge.plugin
    }
  ]
}
