locals { repository_root = abspath("${path.module}/../../..") }

module "contracts" {
  source          = "../../modules/contracts"
  repository_root = local.repository_root
}

locals {
  shared_vars         = module.contracts.shared_vars
  service_mapping     = module.contracts.service_mapping
  cloudflare_zone_ids = module.contracts.cloudflare_zone_ids
}

resource "proxmox_acme_account" "vault" {
  name      = local.acme_account
  contact   = var.cloudflare_owner_email
  directory = local.acme_directory
  tos       = local.acme_terms

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

locals {

  overlay_hosts = { vault = {
    ssh_host       = "hypervisor.home.goodkind.io"
    node_name      = "vault"
    commit         = "2b0a332d39a8876d083c195f7081f73e3988a644"
    guest_api      = true
    kernel_modules = null

    container_options = null

    grub_cmdline_linux_default = null
    network_interfaces_file    = null
  } }
}

module "overlay" {
  source   = "../../modules/overlay"
  for_each = local.overlay_hosts

  ssh_host        = each.value.ssh_host
  node_name       = each.value.node_name
  commit          = each.value.commit
  guest_api       = each.value.guest_api
  automation_user = local.shared_vars.proxmox_api_user
  kernel_modules  = each.value.kernel_modules
  acme_account    = local.acme_account
  acme_plugin     = local.acme_plugin

  container_options = each.value.container_options

  grub_cmdline_linux_default = each.value.grub_cmdline_linux_default
  network_interfaces_file    = each.value.network_interfaces_file
}

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
  acme_plugin_data_version = 2
}

locals { proxmox_token_principal = module.contracts.proxmox_token_principal }
