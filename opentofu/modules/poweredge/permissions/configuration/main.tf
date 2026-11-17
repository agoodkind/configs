module "poweredge" {
  source = "../.."
}

output "lock_targets" {
  value = {
    module_hosts   = { "proxmox_acl.kernel_modules" = "poweredge" }
    lock_endpoints = { poweredge = module.poweredge.lock_endpoint }
  }
}
