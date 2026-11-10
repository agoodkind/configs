module "proxy" { source = "../../services/proxy" }
module "tack" { source = "../../services/tack" }
module "tack_app2" { source = "../../services/tack_app2" }
module "tack_data1" { source = "../../services/tack_data1" }
module "tack_data2" { source = "../../services/tack_data2" }
module "tack_data3" { source = "../../services/tack_data3" }
module "tack_search1" { source = "../../services/tack_search1" }
module "vault_hypervisor" { source = "../../services/vault_hypervisor" }
locals {
  service_mapping = {
    proxy            = module.proxy.value
    tack             = module.tack.value
    tack_app2        = module.tack_app2.value
    tack_data1       = module.tack_data1.value
    tack_data2       = module.tack_data2.value
    tack_data3       = module.tack_data3.value
    tack_search1     = module.tack_search1.value
    tack_k3s         = local.tack_k3s_guest
    vault_hypervisor = module.vault_hypervisor.value
  }
}
output "service_mapping" { value = local.service_mapping }
locals {
  lock_targets = {
    "module_hosts" = {
      "module.vault"    = "vault"
      "module.tack_k3s" = "vault"
    }
  }
}
locals { guest_keys = toset(["tack", "tack_app2", "tack_data1", "tack_data2", "tack_data3", "tack_search1"]) }
output "guest_keys" { value = local.guest_keys }
module "vault_host" { source = "../.." }
output "lock_targets" { value = merge(local.lock_targets, {
  node_hosts  = { for node in toset([for key in local.guest_keys : local.service_mapping[key].node]) : node => "vault" }
  guest_hosts = { "vault/${local.tack_k3s_guest.vmid}" = local.tack_k3s_guest.hostname }
  lock_endpoints = {
    vault                           = module.vault_host.lock_endpoint
    (local.tack_k3s_guest.hostname) = { user = "root", address = local.tack_k3s_guest.ipv6 }
  }
}) }
output "proxy_public_key" { value = module.proxy.upstream_public_key }
output "proxy_address" { value = module.proxy.value.ipv6 }
