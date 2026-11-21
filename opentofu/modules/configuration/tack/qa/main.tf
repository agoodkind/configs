module "suburban_hypervisor" { source = "../../services/suburban_hypervisor" }
module "suburban_host" { source = "../../hosts/suburban" }

locals {
  tack_qa_vmid     = 217
  tack_search_vmid = 227
  container_services = {
    tack_qa_suburban = {
      hostname          = "tack-qa.suburban.goodkind.io"
      node              = "suburban"
      vmid              = local.tack_qa_vmid
      ipv4_cidr         = "${cidrhost(module.suburban_host.vmnet.ipv4_cidr, local.tack_qa_vmid)}/${split("/", module.suburban_host.vmnet.ipv4_cidr)[1]}"
      ipv6              = "3d06:bad:b01:210::217"
      mac_address       = "BC:24:11:04:00:00"
      docker_v6_subnet  = "3d06:bad:b01:210:7ac::/96"
      docker_v6_gateway = "3d06:bad:b01:210:7ac::1"
    }
    tack_search1_suburban = {
      hostname          = "tack-search1.suburban.goodkind.io"
      node              = "suburban"
      vmid              = local.tack_search_vmid
      ipv4_cidr         = "${cidrhost(module.suburban_host.vmnet.ipv4_cidr, local.tack_search_vmid)}/${split("/", module.suburban_host.vmnet.ipv4_cidr)[1]}"
      ipv6              = "3d06:bad:b01:210::227"
      mac_address       = "BC:24:11:04:02:27"
      docker_v6_subnet  = "3d06:bad:b01:210:7b1::/96"
      docker_v6_gateway = "3d06:bad:b01:210:7b1::1"
    }
  }
  service_mapping = merge(local.container_services, {
    tack_k3s_suburban   = local.tack_k3s_guest
    suburban_hypervisor = module.suburban_hypervisor.value
  })
  guest_keys          = toset(["tack_qa_suburban", "tack_search1_suburban"])
  guest_services      = { for key in local.guest_keys : key => local.service_mapping[key] }
  lock_guest_services = merge(local.guest_services, { tack_k3s_suburban = local.tack_k3s_guest })
  lock_targets = {
    module_hosts = {
      "module.tack_k3s_vm"      = "suburban"
      "module.tack_k3s_base"    = "suburban"
      "module.tack_application" = "suburban"
      "module.tack_qa_search"   = "suburban"

      "module.suburban"         = "suburban"
      "module.tack_k3s"         = "suburban"
      "module.tack_qa_services" = "suburban"
    }
    guest_hosts = { for key, service in local.lock_guest_services : "suburban/${service.vmid}" => service.hostname }
    module_key_hosts = {
      "module.base"         = { for key, service in local.guest_services : key => service.hostname }
      "module.docker_guest" = { for key in keys(local.docker_guests) : key => local.service_mapping[key].hostname }
    }
  }
}

output "service_mapping" { value = local.service_mapping }
output "guest_keys" { value = local.guest_keys }
output "lock_targets" {
  value = merge(local.lock_targets, {
    node_hosts = { suburban = "suburban" }
    lock_endpoints = merge({ suburban = module.suburban_host.lock_endpoint }, {
      for key, service in local.lock_guest_services : service.hostname => { user = "root", address = service.ipv6 }
    })
  })
}

locals {
  provisioned_guests = merge(
    { for key in keys(local.lxc_capacity) : key => local.container_services[key] },
    { (local.tack_k3s.guest.service_key) = local.tack_k3s.guest },
  )
}

output "managed_guests" {
  value = { for key, guest in local.provisioned_guests : key => {
    node     = module.suburban_host.node.key
    vmid     = guest.vmid
    hostname = guest.hostname
  } }
}
output "guest_locks" { value = keys(local.lock_guest_services) }
output "keyed_guest_locks" { value = {
  "module.base"         = { for key in local.guest_keys : key => key }
  "module.docker_guest" = { for key in keys(local.docker_guests) : key => key }
} }
