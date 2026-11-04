module "dns64_suburban" { source = "../../services/dns64_suburban" }
module "opnsense_suburban" { source = "../../services/opnsense_suburban" }
module "proxy" { source = "../../../vault/services/proxy" }
module "suburban_hypervisor" { source = "../../services/suburban_hypervisor" }
locals {
  service_mapping = {
    clyde_suburban = {
      "hostname"              = "clyde.suburban.goodkind.io"
      "node"                  = "suburban"
      "vmid"                  = 224
      "ipv6"                  = "3d06:bad:b01:210::224"
      "mac_address"           = "BC:24:11:04:02:24"
      "mail_password_version" = 2
    }
    dns64_suburban      = module.dns64_suburban.value
    opnsense_suburban   = module.opnsense_suburban.value
    proxy               = module.proxy.value
    suburban_hypervisor = module.suburban_hypervisor.value
  }
}
output "service_mapping" { value = local.service_mapping }
locals {
  guest_keys     = toset(["clyde_suburban"])
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
