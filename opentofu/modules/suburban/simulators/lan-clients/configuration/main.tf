module "dns64_suburban" { source = "../../../services/dns64_suburban" }
module "opnsense_suburban" { source = "../../../services/opnsense_suburban" }
locals {
  service_mapping = {
    dns64_suburban = module.dns64_suburban.value
    mwan_test_client_a_suburban = {
      "hostname"    = "mwan-client-a.suburban.goodkind.io"
      "node"        = "suburban"
      "vmid"        = 225
      "ipv4"        = "10.240.1.241"
      "ipv6"        = "3d06:bad:b01:211::225"
      "mac_address" = "BC:24:11:04:02:25"
    }
    mwan_test_client_b_suburban = {
      "hostname"    = "mwan-client-b.suburban.goodkind.io"
      "node"        = "suburban"
      "vmid"        = 226
      "ipv4"        = "10.240.1.242"
      "ipv6"        = "3d06:bad:b01:211::226"
      "mac_address" = "BC:24:11:04:02:26"
    }
    opnsense_suburban = module.opnsense_suburban.value
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
module "suburban_host" { source = "../../.." }
output "lock_targets" { value = merge(local.lock_targets, { lock_endpoints = {
  "suburban" = module.suburban_host.lock_endpoint
} }) }
