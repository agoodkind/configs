output "plane_vmid" {
  description = "VMID assigned to the Plane LXC container"
  value       = proxmox_virtual_environment_container.plane.vm_id
}

output "plane_ipv6" {
  description = "IPv6 address of the Plane LXC container"
  value       = "3d06:bad:b01::115"
}

output "huly_vmid" {
  description = "VMID assigned to the Huly LXC container"
  value       = proxmox_virtual_environment_container.huly.vm_id
}

output "huly_ipv6" {
  description = "IPv6 address of the Huly LXC container"
  value       = "3d06:bad:b01::117"
}
