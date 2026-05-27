output "test_mwan_vmid" {
  description = "VMID assigned to the suburban test MWAN VM."
  value       = proxmox_virtual_environment_vm.vm950_test_mwan.vm_id
}

output "opnsense_test_vmid" {
  description = "VMID assigned to the suburban OPNsense test VM."
  value       = proxmox_virtual_environment_vm.opnsense_test.vm_id
}
