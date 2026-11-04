output "vars" { value = {
  "mwan_proxmox_node" = "hypervisor"
  "mwan_environment" = "testbed"
  "mwan_email_from" = "mwan-testbed@example.invalid"
  "mwan_is_primary" = "no"
  "mwan_vmid" = "{{ service_mapping.testbed_mwan.vmid }}"
  "mwan_agent_addr" = "[{{ service_mapping.testbed_mwan.address }}]:50052"
} }
