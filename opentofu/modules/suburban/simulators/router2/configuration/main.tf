module "proxy" { source = "../../../../vault/services/proxy" }
module "suburban_hypervisor" { source = "../../../services/suburban_hypervisor" }
module "vmbr1_suburban" { source = "../../../services/vmbr1_suburban" }
locals {
  service_mapping = {
    proxy = module.proxy.value
    router2_suburban = {
      "hostname"            = "router2.suburban.goodkind.io"
      "node"                = "suburban"
      "vmid"                = 905
      "ipv4_transit"        = "10.240.240.5"
      "ipv6_transit"        = "3d06:bad:b01:201::5"
      "ipv4_uplink"         = "10.240.200.93"
      "ipv6_uplink"         = "3d06:bad:b01:200::93"
      "announced_prefix_v6" = "3d06:bad:b01:230::/64"
    }
    mwan_suburban = {
      "ipv6_transit" = "3d06:bad:b01:201::3"
    }
    mwan_failover_suburban = {
      "ipv6_transit" = "3d06:bad:b01:201::4"
    }
    suburban_hypervisor = module.suburban_hypervisor.value
    vmbr1_suburban      = module.vmbr1_suburban.value
  }
}
output "service_mapping" { value = local.service_mapping }
output "bgp_asn" { value = 4200000001 }
locals {
  lock_targets = {
    "module_hosts" = {
      "module.suburban" = "suburban"
    }
  }
}
locals { guest_keys = toset(["router2_suburban"]) }
output "guest_keys" { value = local.guest_keys }
module "suburban_host" { source = "../../.." }
output "lock_targets" { value = merge(local.lock_targets, { node_hosts = { for node in toset([for key in local.guest_keys : local.service_mapping[key].node]) : node => "suburban" }, lock_endpoints = {
  "suburban" = module.suburban_host.lock_endpoint
} }) }
output "proxy_public_key" { value = module.proxy.upstream_public_key }
output "proxy_address" { value = module.proxy.value.ipv6 }
