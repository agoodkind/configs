module "proxy" { source = "../../../../vault/services/proxy" }
module "suburban_hypervisor" { source = "../../../services/suburban_hypervisor" }
module "vmbr1_suburban" { source = "../../../services/vmbr1_suburban" }
locals {
  service_mapping = {
    isp_mbrains_suburban = {
      "hostname"    = "isp-mbrains.suburban.goodkind.io"
      "node"        = "suburban"
      "vmid"        = 902
      "ipv6"        = "3d06:bad:b01:250::1"
      "ipv4"        = "10.240.206.1"
      "ipv4_uplink" = "10.240.200.92"
      "ipv6_uplink" = "3d06:bad:b01:200::92"
    }
    proxy               = module.proxy.value
    suburban_hypervisor = module.suburban_hypervisor.value
    vmbr1_suburban      = module.vmbr1_suburban.value
  }
}
output "service_mapping" { value = local.service_mapping }
locals {
  lock_targets = {
    "module_hosts" = {
      "module.suburban" = "suburban"
    }
  }
}
locals { guest_keys = toset(["isp_mbrains_suburban"]) }
output "guest_keys" { value = local.guest_keys }
module "suburban_host" { source = "../../.." }
output "lock_targets" { value = merge(local.lock_targets, { node_hosts = { for node in toset([for key in local.guest_keys : local.service_mapping[key].node]) : node => "suburban" }, lock_endpoints = {
  "suburban" = module.suburban_host.lock_endpoint
} }) }
output "proxy_public_key" { value = module.proxy.upstream_public_key }
output "proxy_address" { value = module.proxy.value.ipv6 }
