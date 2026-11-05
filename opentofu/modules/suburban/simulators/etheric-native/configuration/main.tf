module "proxy" { source = "../../../../vault/services/proxy" }
module "routing_etheric_native_router_suburban" { source = "../../../services/routing_etheric_native_router_suburban" }
module "suburban_hypervisor" { source = "../../../services/suburban_hypervisor" }
module "vmbr1_suburban" { source = "../../../services/vmbr1_suburban" }
locals {
  service_mapping = {
    proxy = module.proxy.value
    routing_etheric_native_client_suburban = {
      "hostname" = "routing-etheric-native-client.suburban.goodkind.io"
      "node"     = "suburban"
      "vmid"     = 911
      "routing_interfaces" = {
        "management" = {
          "name"        = "eth0"
          "network"     = "management"
          "mac_address" = "BC:24:11:52:11:00"
          "ipv4"        = "10.240.200.65"
          "ipv6"        = "3d06:bad:b01:200::65"
        }
        "remote" = {
          "name"        = "eth1"
          "network"     = "etheric_native_remote"
          "mac_address" = "BC:24:11:52:11:01"
          "ipv4"        = ""
          "ipv6"        = "2001:db8:280:1::10"
        }
      }
    }
    routing_etheric_native_router_suburban = module.routing_etheric_native_router_suburban.value
    routing_etheric_native_upstream_suburban = {
      "hostname" = "routing-etheric-native-upstream.suburban.goodkind.io"
      "node"     = "suburban"
      "vmid"     = 910
      "routing_interfaces" = {
        "management" = {
          "name"        = "eth0"
          "network"     = "management"
          "mac_address" = "BC:24:11:52:10:00"
          "ipv4"        = "10.240.200.64"
          "ipv6"        = "3d06:bad:b01:200::64"
        }
        "transit" = {
          "name"        = "eth1"
          "network"     = "etheric_native_transit"
          "mac_address" = "BC:24:11:52:10:01"
          "ipv4"        = ""
          "ipv6"        = "2001:db8:2ff:10::2"
        }
        "remote" = {
          "name"        = "eth2"
          "network"     = "etheric_native_remote"
          "mac_address" = "BC:24:11:52:10:02"
          "ipv4"        = ""
          "ipv6"        = "2001:db8:280:1::1"
        }
      }
    }
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
locals { guest_keys = toset(["routing_etheric_native_client_suburban", "routing_etheric_native_router_suburban", "routing_etheric_native_upstream_suburban"]) }
output "guest_keys" { value = local.guest_keys }
module "suburban_host" { source = "../../.." }
output "lock_targets" { value = merge(local.lock_targets, { node_hosts = { for node in toset([for key in local.guest_keys : local.service_mapping[key].node]) : node => "suburban" }, lock_endpoints = {
  "suburban" = module.suburban_host.lock_endpoint
} }) }
output "proxy_public_key" { value = module.proxy.upstream_public_key }
output "proxy_address" { value = module.proxy.value.ipv6 }
