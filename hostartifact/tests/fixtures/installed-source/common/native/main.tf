variable "workspace" {
  type = string
}

output "lock_targets" {
  value = {
    module_hosts   = { "module.host" = "hypervisor" }
    lock_endpoints = { hypervisor = { local = true } }
  }
}
