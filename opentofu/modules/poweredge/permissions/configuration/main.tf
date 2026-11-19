module "poweredge" {
  source = "../.."
}

output "service_mapping" { value = {} }

output "managed_guests" { value = {} }

output "lock_targets" {
  value = {
    module_hosts = {
      "proxmox_acl.kernel_modules[0]"   = "poweredge"
      "proxmox_acl.acme_certificate[0]" = "poweredge"
    }
    lock_endpoints = { poweredge = module.poweredge.lock_endpoint }
  }
}
