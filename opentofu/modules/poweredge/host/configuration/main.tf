module "poweredge_hypervisor" { source = "../../services/poweredge_hypervisor" }
locals {
  service_mapping = {
    poweredge_hypervisor = module.poweredge_hypervisor.value
  }
}
output "service_mapping" { value = local.service_mapping }
locals {
  lock_targets = {
    "module_hosts" = {
      "module.overlay[\"poweredge\"]"          = "poweredge"
      "proxmox_acme_account.poweredge"         = "poweredge"
      "proxmox_acme_certificate.poweredge"     = "poweredge"
      "proxmox_acme_dns_plugin.poweredge"      = "poweredge"
      "pveguest_host_kernel_modules.poweredge" = "poweredge"
      "linux_download.cloudflared_keyring"     = "poweredge"
      "linux_file"                             = "poweredge"
      "linux_package.cloudflared"              = "poweredge"
      "linux_systemd_unit.cloudflared"         = "poweredge"
      "linux_file_line.hosts"                  = "poweredge"
      "module.unattended_upgrades"             = "poweredge"
      "module.mail"                            = "poweredge"
      "module.smtp2go"                         = "poweredge"
    }
    "node_hosts" = {
      "poweredge" = "poweredge"
    }
  }
}
module "poweredge_host" { source = "../.." }
output "lock_targets" { value = merge(local.lock_targets, { lock_endpoints = {
  "poweredge" = module.poweredge_host.lock_endpoint
} }) }
