module "suburban_hypervisor" { source = "../../../configuration/services/suburban_hypervisor" }
module "vmbr1_suburban" { source = "../../../configuration/services/vmbr1_suburban" }
locals {
  service_mapping = {
    suburban_hypervisor = module.suburban_hypervisor.value
    vmbr1_suburban      = module.vmbr1_suburban.value
  }
}
output "service_mapping" { value = local.service_mapping }
locals {
  lock_targets = {
    "module_hosts" = {
      "module.overlay[\"suburban\"]"           = "suburban"
      "module.suburban"                        = "suburban"
      "proxmox_acme_account.suburban"          = "suburban"
      "proxmox_acme_certificate.suburban"      = "suburban"
      "proxmox_acme_dns_plugin.suburban"       = "suburban"
      "proxmox_storage_directory.local"        = "suburban"
      "proxmox_sdn_applier.suburban"           = "suburban"
      "proxmox_sdn_applier.suburban_finalizer" = "suburban"
      "proxmox_sdn_zone_simple.suburban"       = "suburban"
      "proxmox_sdn_vnet.suburban"              = "suburban"
      "proxmox_sdn_subnet.suburban_ipv4"       = "suburban"
      "proxmox_sdn_subnet.suburban_ipv6"       = "suburban"
      "remote_file"                            = "suburban"
      "terraform_data.uplink_ipv6_activate"    = "suburban"
      "module.unattended_upgrades"             = "suburban"
      "module.mail"                            = "suburban"
      "module.smtp2go"                         = "suburban"
    }
  }
}
module "suburban_host" { source = "../../../configuration/hosts/suburban" }
output "lock_targets" { value = merge(local.lock_targets, { lock_endpoints = {
  "suburban" = module.suburban_host.lock_endpoint
} }) }

output "managed_guests" { value = {} }
