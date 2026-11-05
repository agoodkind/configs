module "dns64_suburban" { source = "../../services/dns64_suburban" }
module "opnsense_suburban" { source = "../../services/opnsense_suburban" }
module "proxy" { source = "../../../vault/services/proxy" }
module "suburban_hypervisor" { source = "../../services/suburban_hypervisor" }
locals {
  service_mapping = {
    dns64_suburban      = module.dns64_suburban.value
    opnsense_suburban   = module.opnsense_suburban.value
    proxy               = module.proxy.value
    suburban_hypervisor = module.suburban_hypervisor.value
    tack_app2_suburban = {
      "hostname"          = "tack-app2.suburban.goodkind.io"
      "node"              = "suburban"
      "vmid"              = 223
      "ipv6"              = "3d06:bad:b01:210::223"
      "mac_address"       = "BC:24:11:04:01:23"
      "docker_v6_subnet"  = "3d06:bad:b01:210:7b0::/96"
      "docker_v6_gateway" = "3d06:bad:b01:210:7b0::1"
    }
    tack_data1_suburban = {
      "hostname"          = "tack-data1.suburban.goodkind.io"
      "node"              = "suburban"
      "vmid"              = 220
      "ipv6"              = "3d06:bad:b01:210::220"
      "mac_address"       = "BC:24:11:04:01:20"
      "docker_v6_subnet"  = "3d06:bad:b01:210:7ad::/96"
      "docker_v6_gateway" = "3d06:bad:b01:210:7ad::1"
    }
    tack_data2_suburban = {
      "hostname"          = "tack-data2.suburban.goodkind.io"
      "node"              = "suburban"
      "vmid"              = 221
      "ipv6"              = "3d06:bad:b01:210::221"
      "mac_address"       = "BC:24:11:04:01:21"
      "docker_v6_subnet"  = "3d06:bad:b01:210:7ae::/96"
      "docker_v6_gateway" = "3d06:bad:b01:210:7ae::1"
    }
    tack_data3_suburban = {
      "hostname"          = "tack-data3.suburban.goodkind.io"
      "node"              = "suburban"
      "vmid"              = 222
      "ipv6"              = "3d06:bad:b01:210::222"
      "mac_address"       = "BC:24:11:04:01:22"
      "docker_v6_subnet"  = "3d06:bad:b01:210:7af::/96"
      "docker_v6_gateway" = "3d06:bad:b01:210:7af::1"
    }
    tack_qa_suburban = {
      "hostname"          = "tack-qa.suburban.goodkind.io"
      "node"              = "suburban"
      "vmid"              = 217
      "ipv6"              = "3d06:bad:b01:210::217"
      "mac_address"       = "BC:24:11:04:00:00"
      "docker_v6_subnet"  = "3d06:bad:b01:210:7ac::/96"
      "docker_v6_gateway" = "3d06:bad:b01:210:7ac::1"
    }
    tack_search1_suburban = {
      "hostname"          = "tack-search1.suburban.goodkind.io"
      "node"              = "suburban"
      "vmid"              = 227
      "ipv6"              = "3d06:bad:b01:210::227"
      "mac_address"       = "BC:24:11:04:02:27"
      "docker_v6_subnet"  = "3d06:bad:b01:210:7b1::/96"
      "docker_v6_gateway" = "3d06:bad:b01:210:7b1::1"
    }
  }
}
output "service_mapping" { value = local.service_mapping }
locals {
  guest_keys     = toset(["tack_app2_suburban", "tack_data1_suburban", "tack_data2_suburban", "tack_data3_suburban", "tack_qa_suburban", "tack_search1_suburban"])
  guest_services = { for key in local.guest_keys : key => local.service_mapping[key] }
}
output "guest_keys" { value = local.guest_keys }
locals {
  lock_targets = merge({
    "module_hosts" = {
      "module.suburban" = "suburban"
    }
  }, { guest_hosts = { for key, service in local.guest_services : "suburban/${service.vmid}" => service.hostname }, module_key_hosts = { "module.base" = { for key, service in local.guest_services : key => service.hostname } } })
}
module "suburban_host" { source = "../.." }
output "lock_targets" { value = merge(local.lock_targets, { node_hosts = { for node in toset([for key in local.guest_keys : local.service_mapping[key].node]) : node => "suburban" }, lock_endpoints = merge({ suburban = module.suburban_host.lock_endpoint }, { for key, service in local.guest_services : service.hostname => { user = "root", address = service.ipv6 } }) }) }
output "proxy_public_key" { value = module.proxy.upstream_public_key }
output "proxy_address" { value = module.proxy.value.ipv6 }
