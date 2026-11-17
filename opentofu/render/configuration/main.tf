variable "workspace" {
  type  = string
  const = true
}
module "configuration" {
  source    = "../../modules/configuration/workspace"
  workspace = var.workspace
}
output "lock_targets" { value = module.configuration.value.lock_targets }

output "guest_references" {
  value = {
    source = "${module.configuration.source_directory}/main.tf"
    declared = { for key, guest in module.configuration.value.managed_guests : key => {
      node = guest.node, vmid = guest.vmid, hostname = guest.hostname
    } }
    services = { for key, service in module.configuration.value.service_mapping : key => {
      node = try(service.node, null), vmid = try(service.vmid, null), hostname = try(service.hostname, null)
    } if can(service.node) || can(service.vmid) }
    locks       = try(module.configuration.value.guest_locks, [])
    keyed_locks = try(module.configuration.value.keyed_guest_locks, {})
  }
}
