output "vmid" {
  description = "Proxmox VM ID of the k3s server."
  value       = proxmox_virtual_environment_vm.server.vm_id
}

output "node" {
  description = "Provider node key for guest management."
  value       = var.guest.node
}

output "hostname" {
  description = "DNS hostname of the k3s server."
  value       = var.guest.hostname
}

output "ipv4" {
  description = "Static IPv4 address of the VM, or null when the guest does not configure one."
  value       = var.guest.ipv4_cidr == null ? null : split("/", var.guest.ipv4_cidr)[0]
}

output "ipv6" {
  description = "Static IPv6 address of the VM and Kubernetes node."
  value       = split("/", var.guest.ipv6_cidr)[0]
}

output "guest" {
  value = merge(var.guest, { vmid = proxmox_virtual_environment_vm.server.vm_id })
}
