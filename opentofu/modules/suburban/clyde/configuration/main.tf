module "suburban_hypervisor" { source = "../../../configuration/services/suburban_hypervisor" }
locals {
  container_services = {
    clyde_suburban = {
      "hostname"              = "clyde.suburban.goodkind.io"
      "node"                  = "suburban"
      "vmid"                  = 224
      "ipv6"                  = "3d06:bad:b01:210::224"
      "mac_address"           = "BC:24:11:04:02:24"
      "mail_password_version" = 2
    }
  }
  service_mapping = merge(local.container_services, {
    suburban_hypervisor = module.suburban_hypervisor.value
  })
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
module "suburban_host" { source = "../../../configuration/hosts/suburban" }
output "lock_targets" { value = merge(local.lock_targets, { node_hosts = { for node in toset([for key in local.guest_keys : local.service_mapping[key].node]) : node => "suburban" }, lock_endpoints = merge({ suburban = module.suburban_host.lock_endpoint }, { for key, service in local.guest_services : service.hostname => { user = "root", address = service.ipv6 } }) }) }

output "managed_guests" {
  value = { for key, guest in local.container_services : key => {
    node     = module.suburban_host.node.key
    vmid     = guest.vmid
    hostname = guest.hostname
  } }
}
output "guest_locks" { value = keys(local.guest_services) }
output "keyed_guest_locks" { value = { "module.base" = { for key in local.guest_keys : key => key } } }
