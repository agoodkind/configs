output "tack_vmid" {
  description = "VMID assigned to the Tack LXC container."
  value       = proxmox_virtual_environment_container.tack.vm_id
}

output "tack_ipv6" {
  description = "IPv6 address of the Tack LXC container."
  value       = "3d06:bad:b01::117"
}
