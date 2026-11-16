locals { failover_management_ipv6 = "3d06:bad:b01:210::216" }

module "isp_routed_suburban" { source = "../../../configuration/services/isp_routed_suburban" }
module "isp_sonic_suburban" { source = "../../../configuration/services/isp_sonic_suburban" }
module "isp_webpass_suburban" { source = "../../../configuration/services/isp_webpass_suburban" }
module "opnsense_suburban" { source = "../../../configuration/services/opnsense_suburban" }
module "proxy" { source = "../../../configuration/services/proxy" }
module "routing_astound_static_endpoint_suburban" { source = "../../../configuration/services/routing_astound_static_endpoint_suburban" }
module "routing_etheric_native_router_suburban" { source = "../../../configuration/services/routing_etheric_native_router_suburban" }
module "routing_tunnel_static_vps_suburban" { source = "../../../configuration/services/routing_tunnel_static_vps_suburban" }
module "routing_tunnel_upstream_upstream_suburban" { source = "../../../configuration/services/routing_tunnel_upstream_upstream_suburban" }
module "routing_tunnel_upstream_vps_suburban" { source = "../../../configuration/services/routing_tunnel_upstream_vps_suburban" }
module "routing_tunnel_vps_vps_suburban" { source = "../../../configuration/services/routing_tunnel_vps_vps_suburban" }
module "suburban_hypervisor" { source = "../../../configuration/services/suburban_hypervisor" }
locals {
  service_mapping = {
    isp_routed_suburban  = module.isp_routed_suburban.value
    isp_sonic_suburban   = module.isp_sonic_suburban.value
    isp_webpass_suburban = module.isp_webpass_suburban.value
    mwan_failover_suburban = {
      "hostname"     = "mwan-failover.suburban.goodkind.io"
      "node"         = "suburban"
      "vmid"         = 216
      "ipv4_transit" = "10.240.240.4"
      "ipv6_transit" = "3d06:bad:b01:201::4"
      "ipv4_vmnet"   = "10.240.4.216"
      "ipv6_vmnet"   = local.failover_management_ipv6
      "ipv6"         = local.failover_management_ipv6
    }
    mwan_suburban = {
      "hostname"     = "mwan.suburban.goodkind.io"
      "node"         = "suburban"
      "vmid"         = 213
      "kind"         = "qemu"
      "ipv6"         = "3d06:bad:b01:210::213"
      "ipv4_transit" = "10.240.240.3"
      "ipv6_transit" = "3d06:bad:b01:201::3"
      "ipv4_astound" = "10.240.207.2"
      "ipv4_webpass" = "10.241.204.2"
      "ipv4_routed"  = "10.240.208.2"
      "ipv6_routed"  = "3d06:bad:b01:260::2"
      "routing_interfaces" = {
        "sonic_1" = {
          "network"     = "sonic_1"
          "mac_address" = "BC:24:11:52:00:01"
          "ipv4"        = "10.240.209.2"
          "ipv6"        = ""
        }
        "sonic_2" = {
          "network"     = "sonic_2"
          "mac_address" = "BC:24:11:52:00:02"
          "ipv4"        = "10.240.210.2"
          "ipv6"        = ""
        }
        "etheric" = {
          "network"     = "etheric"
          "mac_address" = "BC:24:11:52:00:03"
          "ipv4"        = "10.240.211.2"
          "ipv6"        = "2001:db8:2ff:20::2"
        }
      }
    }
    opnsense_suburban                         = module.opnsense_suburban.value
    proxy                                     = module.proxy.value
    routing_astound_static_endpoint_suburban  = module.routing_astound_static_endpoint_suburban.value
    routing_etheric_native_router_suburban    = module.routing_etheric_native_router_suburban.value
    routing_tunnel_static_vps_suburban        = module.routing_tunnel_static_vps_suburban.value
    routing_tunnel_upstream_upstream_suburban = module.routing_tunnel_upstream_upstream_suburban.value
    routing_tunnel_upstream_vps_suburban      = module.routing_tunnel_upstream_vps_suburban.value
    routing_tunnel_vps_vps_suburban           = module.routing_tunnel_vps_vps_suburban.value
    suburban_hypervisor                       = module.suburban_hypervisor.value
  }
}
output "service_mapping" { value = local.service_mapping }
locals { guest_keys = toset([]) }
output "guest_keys" { value = local.guest_keys }
locals {
  lock_targets = {
    "module_hosts" = {
      "module.mwan"          = "suburban"
      "module.mwan_failover" = "suburban"
      "module.suburban"      = "suburban"
    }
    "lock_free_types" = ["mwan_network_config", "terraform_data"]
  }
}
module "suburban_host" { source = "../../../configuration/hosts/suburban" }
output "lock_targets" { value = merge(local.lock_targets, { node_hosts = { suburban = "suburban" }, lock_endpoints = { suburban = module.suburban_host.lock_endpoint } }) }
output "proxy_public_key" { value = module.proxy.upstream_public_key }
output "proxy_address" { value = module.proxy.value.ipv6 }
