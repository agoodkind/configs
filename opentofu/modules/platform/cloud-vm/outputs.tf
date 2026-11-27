locals {
  # The provider computes id when Proxmox creates the VM. Guest resources must
  # reinstall on a fresh disk even when the new VM uses the same numeric VM ID.
  vmid = var.smbios_family == null ? proxmox_virtual_environment_vm.server.vm_id : tonumber(proxmox_virtual_environment_vm.server.id)
}

output "vmid" {
  description = "Proxmox VM ID of the k3s server."
  value       = local.vmid
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
  value       = var.guest.ipv6_cidr == null ? null : split("/", var.guest.ipv6_cidr)[0]
}

output "cloud_image_id" {
  value = var.cloud_image == null ? var.shared_cloud_image_id : proxmox_download_file.cloud_image[0].id
}

output "guest" {
  value = merge(var.guest, { vmid = local.vmid })
}

output "build_uuid" {
  description = "Proxmox SMBIOS UUID of this VM build when smbios_family is configured."
  value       = var.smbios_family == null ? null : one(proxmox_virtual_environment_vm.server.smbios).uuid
}
