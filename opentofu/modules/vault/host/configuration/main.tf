locals {
  service_mapping = {

  }
}
output "service_mapping" { value = local.service_mapping }
locals {
  lock_targets = {
    "module_hosts" = {
      "module.overlay[\"vault\"]"                          = "vault"
      "proxmox_acme_account.vault"                         = "vault"
      "proxmox_acme_certificate.vault"                     = "vault"
      "proxmox_acme_dns_plugin.vault"                      = "vault"
      "proxmox_virtual_environment_dns.vault"              = "vault"
      "remote_file.guest_network[\"bridge\"]"              = "vault"
      "remote_file.guest_network[\"firewall\"]"            = "vault"
      "remote_file.guest_network[\"firewall_unit\"]"       = "vault"
      "remote_file.guest_network[\"forwarding\"]"          = "vault"
      "remote_file.guest_network[\"private_tunnel_unit\"]" = "vault"
      "remote_file.guest_network[\"uplink\"]"              = "vault"
      "terraform_data.guest_network_packages"              = "vault"
      "terraform_data.guest_network_activate"              = "vault"
    }
  }
}
module "vault_host" { source = "../.." }
output "lock_targets" { value = merge(local.lock_targets, { lock_endpoints = {
  "vault" = module.vault_host.lock_endpoint
} }) }
