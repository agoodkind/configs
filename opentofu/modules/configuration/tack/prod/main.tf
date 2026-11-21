module "proxy" { source = "../../services/proxy" }
module "tack" { source = "../../services/tack" }
module "tack_app2" { source = "../../services/tack_app2" }
module "tack_data1" { source = "../../services/tack_data1" }
module "tack_data2" { source = "../../services/tack_data2" }
module "tack_data3" { source = "../../services/tack_data3" }
module "tack_search1" { source = "../../services/tack_search1" }
module "vault_hypervisor" { source = "../../services/vault_hypervisor" }
module "seaweedfs" { source = "../../services/seaweedfs" }
locals {
  container_services = {
    tack         = module.tack.value
    tack_app2    = module.tack_app2.value
    tack_data1   = module.tack_data1.value
    tack_data2   = module.tack_data2.value
    tack_data3   = module.tack_data3.value
    tack_search1 = module.tack_search1.value
  }
  service_mapping = merge(local.container_services, {
    tack_k3s         = local.tack_k3s_guest
    proxy            = module.proxy.value
    vault_hypervisor = module.vault_hypervisor.value
  })
}
output "service_mapping" { value = local.service_mapping }
locals {
  static_guest_locks = {
    "linux_file_line.compose_search"          = "tack"
    "linux_file_line.search_firewall_include" = "tack_search1"
  }
  search_lock_guests = {
    "linux_file_line.compose_search"          = module.tack.value
    "linux_file_line.search_firewall_include" = module.tack_search1.value
  }
  # These operations use the Proxmox API while the guest's private SSH address is unavailable.
  network_lock_hosts = merge([for key in concat(tolist(local.guest_keys), ["tack_k3s"]) : {
    for resource in concat([
      "pveguest_apt_packages.network",
      "pveguest_network_interface.eth0",
    ], module.vault_host.guest_networks[key].kind == "lxc" ? ["pveguest_container_options.hostnic[0]"] : []) :
    "module.guest_network[${jsonencode(key)}].${resource}" => "vault"
  }]...)
  lock_targets = {
    module_hosts = merge({
      "module.tack_k3s_vm"      = "vault"
      "module.tack_k3s_base"    = "vault"
      "module.tack_application" = "vault"

      "module.vault"                 = "vault"
      "proxmox_hardware_mapping_pci" = "vault"
      "module.tack_k3s"              = "vault"
    }, local.network_lock_hosts, { for address, guest in local.search_lock_guests : address => guest.hostname })
  }
}
locals { guest_keys = toset(["tack", "tack_app2", "tack_data1", "tack_data2", "tack_data3", "tack_search1"]) }
output "guest_keys" { value = local.guest_keys }
module "vault_host" { source = "../../hosts/vault" }
output "lock_targets" { value = merge(local.lock_targets, {
  node_hosts = { for node in toset([for key in local.guest_keys : local.service_mapping[key].node]) : node => "vault" }
  guest_hosts = merge({ "vault/${local.tack_k3s_guest.vmid}" = local.tack_k3s_guest.hostname }, {
    for guest in local.search_lock_guests : "${guest.node}/${guest.vmid}" => guest.hostname
  })
  lock_endpoints = merge({
    vault                           = module.vault_host.lock_endpoint
    (local.tack_k3s_guest.hostname) = { user = "root", address = local.tack_k3s_guest.ipv6 }
  }, { for guest in local.search_lock_guests : guest.hostname => { user = "root", address = guest.ipv6 } })
}) }
output "proxy_public_key" { value = module.proxy.upstream_public_key }
output "proxy_address" { value = module.proxy.value.ipv6 }

locals {
  provisioned_guests = merge(
    local.container_services,
    { (local.tack_k3s.guest.service_key) = local.tack_k3s.guest },
  )
}

output "managed_guests" {
  value = { for key, guest in local.provisioned_guests : key => {
    node     = module.vault_host.node.key
    vmid     = guest.vmid
    hostname = guest.hostname
  } }
}
output "static_guest_locks" { value = local.static_guest_locks }
output "guest_locks" { value = concat([local.tack_k3s_guest.service_key], values(local.static_guest_locks)) }
