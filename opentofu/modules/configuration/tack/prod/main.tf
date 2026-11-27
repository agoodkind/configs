module "tack" { source = "../../services/tack" }
module "tack_data1" { source = "../../services/tack_data1" }
module "tack_data2" { source = "../../services/tack_data2" }
module "tack_data3" { source = "../../services/tack_data3" }
module "tack_search1" { source = "../../services/tack_search1" }
module "vault_hypervisor" { source = "../../services/vault_hypervisor" }
module "seaweedfs" { source = "../../services/seaweedfs" }
locals {
  container_services = {
    tack         = module.tack.value
    tack_data1   = module.tack_data1.value
    tack_data2   = module.tack_data2.value
    tack_data3   = module.tack_data3.value
    tack_search1 = module.tack_search1.value
  }
  service_mapping = merge(local.container_services, {
    tack_k3s         = local.tack_k3s_guest
    vault_hypervisor = module.vault_hypervisor.value
  })
}
output "service_mapping" { value = local.service_mapping }
locals {
  static_guest_locks = {
    "linux_file_line.compose_files"           = "tack"
    "linux_file_line.compose_image_tag"       = "tack"
    "linux_file_line.compose_search"          = "tack"
    "linux_file_line.search_firewall_include" = "tack_search1"
  }
  locked_guests = { for key in toset(values(local.static_guest_locks)) : key => local.service_mapping[key] }
  # These operations use the Proxmox API while the guest's private SSH address is unavailable.
  network_lock_hosts = merge([for key in concat(tolist(local.guest_keys), ["tack_k3s"]) : {
    for resource in concat([
      "pveguest_apt_packages.network",
      "pveguest_network_interface.eth0",
    ], module.vault_host.guest_networks[key].kind == "lxc" ? ["pveguest_container_options.hostnic[0]"] : []) :
    "module.guest_network[${jsonencode(key)}].${resource}" => "vault"
  }]...)
  worker_vm_locks = merge([for key in keys(local.k3s_workers) : {
    "module.k3s_worker_vm[${jsonencode(key)}]"                            = "vault"
    "proxmox_virtual_environment_file.worker_network[${jsonencode(key)}]" = "vault"
  }]...)
  lock_targets = {
    module_hosts = merge({
      "module.tack_k3s_vm"      = "vault"
      "module.tack_k3s_base"    = "vault"
      "module.tack_application" = "vault"

      "module.vault"                 = "vault"
      "proxmox_hardware_mapping_pci" = "vault"
      "module.tack_k3s"              = "vault"
    }, local.worker_vm_locks, local.network_lock_hosts, { for address, key in local.static_guest_locks : address => local.locked_guests[key].hostname })
  }
}
locals { guest_keys = toset(["tack", "tack_data1", "tack_data2", "tack_data3", "tack_search1"]) }
output "guest_keys" { value = local.guest_keys }
module "vault_host" { source = "../../hosts/vault" }
output "lock_targets" { value = merge(local.lock_targets, {
  node_hosts = { for node in toset([for key in local.guest_keys : local.service_mapping[key].node]) : node => "vault" }
  guest_hosts = merge({ "vault/${local.tack_k3s_guest.vmid}" = local.tack_k3s_guest.hostname }, {
    for guest in local.locked_guests : "${guest.node}/${guest.vmid}" => guest.hostname
  })
  lock_endpoints = merge({
    vault                           = module.vault_host.lock_endpoint
    (local.tack_k3s_guest.hostname) = { user = "root", address = local.tack_k3s_guest.ipv6 }
  }, { for guest in local.locked_guests : guest.hostname => { user = "root", address = guest.ipv6 } })
}) }

locals {
  provisioned_guests = merge(
    local.container_services,
    local.k3s_workers,
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
output "guest_locks" { value = concat([local.tack_k3s_guest.service_key], keys(local.locked_guests)) }
