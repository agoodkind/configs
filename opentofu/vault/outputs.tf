output "tack_vmid" {
  description = "VMID assigned to the Tack LXC container."
  value       = proxmox_virtual_environment_container.tack.vm_id
}

output "tack_ipv6" {
  description = "IPv6 address of the Tack LXC container."
  value       = "3d06:bad:b01::117"
}

output "seaweedfs_vmid" {
  description = "VMID assigned to the vault SeaweedFS LXC."
  value       = proxmox_virtual_environment_container.seaweedfs.vm_id
}

output "seaweedfs_ipv6" {
  description = "IPv6 address of the vault SeaweedFS LXC."
  value       = "3d06:bad:b01::118"
}
