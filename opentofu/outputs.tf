output "tack_vmid" {
  description = "VMID assigned to the Tack LXC container on vault"
  value       = module.vault.tack_vmid
}

output "tack_ipv6" {
  description = "IPv6 address of the Tack LXC container on vault"
  value       = module.vault.tack_ipv6
}

output "tack_qa_suburban_vmid" {
  description = "VMID assigned to the Tack QA LXC container on suburban"
  value       = module.suburban.tack_qa_suburban_vmid
}

output "tack_qa_suburban_ipv6" {
  description = "IPv6 address of the Tack QA LXC container on suburban"
  value       = module.suburban.tack_qa_suburban_ipv6
}

output "seaweedfs_suburban_vmid" {
  description = "VMID assigned to the suburban SeaweedFS LXC container"
  value       = module.suburban.seaweedfs_suburban_vmid
}

output "seaweedfs_suburban_ipv6" {
  description = "IPv6 address of the suburban SeaweedFS LXC container"
  value       = module.suburban.seaweedfs_suburban_ipv6
}

output "dns64_suburban_vmid" {
  description = "VMID assigned to the suburban DNS64 LXC"
  value       = module.suburban.dns64_suburban_vmid
}

output "dns64_suburban_ipv6" {
  description = "IPv6 address of the suburban DNS64 LXC"
  value       = module.suburban.dns64_suburban_ipv6
}

output "mwan_suburban_vmid" {
  description = "VMID assigned to the suburban MWAN VM"
  value       = module.suburban.mwan_suburban_vmid
}

output "opnsense_suburban_vmid" {
  description = "VMID assigned to the suburban OPNsense VM"
  value       = module.suburban.opnsense_suburban_vmid
}
