module "poweredge" {
  source = "../.."
}

output "lock_targets" {
  value = {
    module_hosts   = { "proxmox_acl.kernel_modules[0]" = "poweredge" }
    lock_endpoints = { poweredge = module.poweredge.lock_endpoint }
  }
}
