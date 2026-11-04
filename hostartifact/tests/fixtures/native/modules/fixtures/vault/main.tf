output "vars" { value = {
  "mwan_proxmox_node" = "vault"
  "mwan_environment" = "prod"
  "mwan_email_from" = "mwan@example.invalid"
  "mwan_is_primary" = "yes"
  "mwan_vmid" = "{{ service_mapping.mwan.vmid }}"
  "mwan_agent_addr" = "[{{ service_mapping.mwan.address }}]:50052"
} }
