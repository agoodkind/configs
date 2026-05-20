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
  value       = var.tack_qa_ipv6_address
}
