module "proxy" { source = "../../../../vault/services/proxy" }
module "routing_tunnel_upstream_upstream_suburban" { source = "../../../services/routing_tunnel_upstream_upstream_suburban" }
module "routing_tunnel_upstream_vps_suburban" { source = "../../../services/routing_tunnel_upstream_vps_suburban" }
module "suburban_hypervisor" { source = "../../../services/suburban_hypervisor" }
module "vmbr1_suburban" { source = "../../../services/vmbr1_suburban" }
locals {
  service_mapping = {
    proxy = module.proxy.value
    routing_tunnel_upstream_client_suburban = {
      "hostname" = "routing-tunnel-upstream-client.suburban.goodkind.io"
      "node"     = "suburban"
      "vmid"     = 917
      "routing_interfaces" = {
        "management" = {
          "name"        = "eth0"
          "network"     = "management"
          "mac_address" = "BC:24:11:52:17:00"
          "ipv4"        = "10.240.200.71"
          "ipv6"        = "3d06:bad:b01:200::71"
        }
        "remote" = {
          "name"        = "eth1"
          "network"     = "tunnel_upstream_remote"
          "mac_address" = "BC:24:11:52:17:01"
          "ipv4"        = ""
          "ipv6"        = "2001:db8:480:1::10"
        }
      }
    }
    routing_tunnel_upstream_upstream_suburban = module.routing_tunnel_upstream_upstream_suburban.value
    routing_tunnel_upstream_vps_suburban      = module.routing_tunnel_upstream_vps_suburban.value
    suburban_hypervisor                       = module.suburban_hypervisor.value
    vmbr1_suburban                            = module.vmbr1_suburban.value
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
locals { guest_keys = toset(["routing_tunnel_upstream_client_suburban", "routing_tunnel_upstream_upstream_suburban", "routing_tunnel_upstream_vps_suburban"]) }
output "guest_keys" { value = local.guest_keys }
module "suburban_host" { source = "../../.." }
output "lock_targets" { value = merge(local.lock_targets, { node_hosts = { for node in toset([for key in local.guest_keys : local.service_mapping[key].node]) : node => "suburban" }, lock_endpoints = {
  "suburban" = module.suburban_host.lock_endpoint
} }) }
output "proxy_public_key" { value = module.proxy.upstream_public_key }
output "proxy_address" { value = module.proxy.value.ipv6 }
