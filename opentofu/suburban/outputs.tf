output "mwan_suburban_vmid" {
  description = "VMID assigned to the suburban MWAN VM."
  value       = proxmox_virtual_environment_vm.mwan_suburban.vm_id
}

output "opnsense_suburban_vmid" {
  description = "VMID assigned to the suburban OPNsense VM."
  value       = proxmox_virtual_environment_vm.opnsense_suburban.vm_id
}

output "tack_qa_suburban_vmid" {
  description = "VMID assigned to the suburban Tack QA LXC."
  value       = proxmox_virtual_environment_container.tack_qa_suburban.vm_id
}

output "tack_qa_suburban_ipv6" {
  description = "IPv6 address assigned to the suburban Tack QA LXC."
  value       = local.service_mapping.tack_qa_suburban.ipv6
}

output "seaweedfs_suburban_vmid" {
  description = "VMID assigned to the suburban SeaweedFS LXC."
  value       = proxmox_virtual_environment_container.seaweedfs_suburban.vm_id
}

output "seaweedfs_suburban_ipv6" {
  description = "IPv6 address assigned to the suburban SeaweedFS LXC."
  value       = local.service_mapping.seaweedfs_suburban.ipv6
}

output "dns64_suburban_vmid" {
  description = "VMID assigned to the suburban DNS64 LXC."
  value       = proxmox_virtual_environment_container.dns64_suburban.vm_id
}

output "dns64_suburban_ipv6" {
  description = "IPv6 address assigned to the suburban DNS64 LXC."
  value       = local.service_mapping.dns64_suburban.ipv6
}
