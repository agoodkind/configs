module "suburban_hypervisor" { source = "../../services/suburban_hypervisor" }
module "vmbr1_suburban" { source = "../../services/vmbr1_suburban" }
locals {
  service_mapping = {
    suburban_hypervisor = module.suburban_hypervisor.value
    vmbr1_suburban      = module.vmbr1_suburban.value
    vmbrtrunk_suburban = {
      "ipv4" = "10.240.4.5"
      "ipv6" = "3d06:bad:b01:210::5"
    }
  }
}
output "service_mapping" { value = local.service_mapping }
locals {
  lock_targets = {
    "module_hosts" = {
      "module.overlay[\"suburban\"]"      = "suburban"
      "module.suburban"                   = "suburban"
      "proxmox_acme_account.suburban"     = "suburban"
      "proxmox_acme_certificate.suburban" = "suburban"
      "proxmox_acme_dns_plugin.suburban"  = "suburban"
      "proxmox_storage_directory.local"   = "suburban"
    }
  }
}
module "suburban_host" { source = "../.." }
output "lock_targets" { value = merge(local.lock_targets, { lock_endpoints = {
  "suburban" = module.suburban_host.lock_endpoint
} }) }
