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
  description = "Static IPv4 address of the VM."
  value       = split("/", var.guest.ipv4_cidr)[0]
}

output "ipv6" {
  description = "Static IPv6 address of the VM and Kubernetes node."
  value       = local.node_ipv6
}

output "cluster" {
  description = "Nonsecret cluster endpoint, network, version, and configuration identity."
  value = {
    endpoint             = "https://[${local.node_ipv6}]:6443"
    pod_cidr             = var.cluster_network.pod_cidr
    service_cidr         = var.cluster_network.service_cidr
    ip_family_policy     = "SingleStack"
    ip_families          = ["IPv6"]
    k3s_version          = var.k3s_artifact.version
    configuration_sha256 = sha256(yamlencode(local.cluster_configuration))
  }
}
