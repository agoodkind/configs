locals {
  service_mapping = {

  }
}
output "service_mapping" { value = local.service_mapping }
locals {
  lock_targets = {
    "module_hosts" = {
      "module.overlay[\"vault\"]"             = "vault"
      "proxmox_acme_account.vault"            = "vault"
      "proxmox_acme_certificate.vault"        = "vault"
      "proxmox_acme_dns_plugin.vault"         = "vault"
      "proxmox_storage_directory.local"       = "vault"
      "proxmox_virtual_environment_dns.vault" = "vault"
      "proxmox_sdn_applier.vault"             = "vault"
      "proxmox_sdn_applier.vault_finalizer"   = "vault"
      "proxmox_sdn_zone_simple.vault"         = "vault"
      "proxmox_sdn_vnet.vault"                = "vault"
      "proxmox_sdn_subnet.vault_ipv4"         = "vault"
      "proxmox_sdn_subnet.vault_ipv6"         = "vault"
      "remote_file"                           = "vault"
      "terraform_data.guest_network_packages" = "vault"
      "terraform_data.guest_network_activate" = "vault"
    }
  }
}
module "vault_host" { source = "../.." }
output "lock_targets" { value = merge(local.lock_targets, { lock_endpoints = {
  "vault" = module.vault_host.lock_endpoint
} }) }
