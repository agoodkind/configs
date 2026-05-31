output "test_mwan_vmid" {
  description = "VMID assigned to the suburban test MWAN VM."
  value       = proxmox_virtual_environment_vm.vm950_test_mwan.vm_id
}

output "opnsense_test_vmid" {
  description = "VMID assigned to the suburban OPNsense test VM."
  value       = proxmox_virtual_environment_vm.opnsense_test.vm_id
}

output "tack_qa_vmid" {
  description = "VMID assigned to the suburban Tack QA LXC."
  value       = proxmox_virtual_environment_container.tack_qa.vm_id
}

output "tack_qa_ipv6" {
  description = "IPv6 address assigned to the suburban Tack QA LXC."
  value       = var.tack_qa.ipv6_address
}

output "seaweedfs_vmid" {
  description = "VMID assigned to the suburban SeaweedFS LXC."
  value       = proxmox_virtual_environment_container.seaweedfs.vm_id
}

output "seaweedfs_ipv6" {
  description = "IPv6 address assigned to the suburban SeaweedFS LXC."
  value       = var.seaweedfs.ipv6_address
}

output "dns64_vmid" {
  description = "VMID assigned to the suburban DNS64 LXC."
  value       = proxmox_virtual_environment_container.dns64.vm_id
}

output "dns64_ipv6" {
  description = "IPv6 address assigned to the suburban DNS64 LXC."
  value       = var.dns64.ipv6_address
}
