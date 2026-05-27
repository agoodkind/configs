output "tack_vmid" {
  description = "VMID assigned to the Tack LXC container on vault"
  value       = module.vault.tack_vmid
}

output "tack_ipv6" {
  description = "IPv6 address of the Tack LXC container on vault"
  value       = module.vault.tack_ipv6
}

output "tack_qa_vmid" {
  description = "VMID assigned to the Tack QA LXC container on suburban"
  value       = module.suburban.tack_qa_vmid
}

output "tack_qa_ipv6" {
  description = "IPv6 address of the Tack QA LXC container on suburban"
  value       = module.suburban.tack_qa_ipv6
}

output "test_mwan_vmid" {
  description = "VMID assigned to the suburban test MWAN VM"
  value       = module.suburban.test_mwan_vmid
}

output "opnsense_test_vmid" {
  description = "VMID assigned to the suburban OPNsense test VM"
  value       = module.suburban.opnsense_test_vmid
}
