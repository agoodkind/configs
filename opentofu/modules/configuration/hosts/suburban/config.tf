locals {
  vmnet = {
    bridge       = "vmnet"
    ipv4_cidr    = "10.240.5.0/24"
    ipv4_gateway = "10.240.5.254"
    ipv6_cidr    = "3d06:bad:b01:210::/64"
    ipv6_gateway = "3d06:bad:b01:210::254"
  }

  bridges = {
    vm_management_suburban = "vmbr1"
  }
}

output "bridges" {
  description = "Suburban host management bridge."
  value       = local.bridges
}

output "vmnet" {
  value = local.vmnet
}
