module "vault_host" { source = "../../../configuration/hosts/vault" }
module "artifacts" { source = "../../../configuration/artifacts" }
module "vault_hypervisor" { source = "../../../configuration/services/vault_hypervisor" }

locals {
  network = module.vault_host.guest_networks.terrakube
  guest = merge(module.vault_host.guest, {
    node        = module.vault_host.node.key
    vmid        = local.network.vmid
    hostname    = "terrakube.vault.goodkind.io"
    service_key = "terrakube"
    mac_address = local.network.mac
    ipv6_cidr   = local.network.addresses[0]
  })
  identity = {
    node     = local.guest.node
    vmid     = local.guest.vmid
    hostname = local.guest.hostname
  }
  dashboard = {
    hostname = "dashboard.tf.infra.goodkind.io"
    ipv6     = split("/", local.network.addresses[1])[0]
    route    = local.network.addresses[1]
  }
}

output "guest" { value = local.guest }
output "network" { value = merge(local.network, { pf = module.vault_host.guest_pf }) }
output "dashboard" { value = local.dashboard }
output "webhook" {
  value = {
    hostname = "hooks.tf.infra.goodkind.io"
    path     = "^/webhook/v2/[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$"
    origin   = "http://[::1]:8081"
  }
}
output "namespace" { value = "terrakube" }
output "cloudflared_image" { value = module.artifacts.cloudflared_image }
output "cloud_image" { value = module.artifacts.debian_generic_image }
output "k3s_artifact" { value = module.artifacts.k3s }
output "k3s_network" {
  value = {
    pod_cidr     = "fd42:7466:1::/56"
    service_cidr = "fd42:7466:2::/112"
    dns_address  = "fd42:7466:2::a"
  }
}
output "managed_guests" { value = { terrakube = local.identity } }
output "service_mapping" { value = { terrakube = local.identity, vault_hypervisor = module.vault_hypervisor.value } }
output "guest_locks" { value = ["terrakube"] }
output "lock_targets" {
  value = {
    node_hosts = { vault = "vault" }
    module_hosts = {
      "module.vm"                    = "vault"
      "module.network"               = "vault"
      "module.base"                  = "vault"
      "module.k3s"                   = "vault"
      "pveguest_file"                = "vault"
      "pveguest_download"            = "vault"
      "proxmox_hardware_mapping_pci" = "vault"
    }
    guest_hosts = { "vault/${local.guest.vmid}" = local.guest.hostname }
    lock_endpoints = {
      vault                  = module.vault_host.lock_endpoint
      (local.guest.hostname) = { user = "root", address = split("/", local.guest.ipv6_cidr)[0] }
    }
  }
}
