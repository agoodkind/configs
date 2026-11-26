module "terrakube" { source = "../../../vault/terrakube-prod/configuration" }

output "service_mapping" { value = module.terrakube.service_mapping }
locals {
  lock_targets = {
    "lock_free_types" = ["cloudflare_zero_trust_gateway_policy", "cloudflare_zero_trust_device_custom_profile", "cloudflare_zero_trust_device_managed_networks", "cloudflare_zero_trust_tunnel_cloudflared", "cloudflare_zero_trust_tunnel_cloudflared_route", "cloudflare_zero_trust_tunnel_cloudflared_config"]
    module_hosts      = { "module.terrakube_cloudflared" = "vault" }
    module_key_hosts = {
      "module.tack_cloudflared" = { for environment, connector in local.tack_connectors : environment => connector.guest.hostname }
    }
    node_hosts  = merge({ vault = "vault" }, { for connector in values(local.tack_connectors) : connector.guest.node => connector.guest.node })
    guest_hosts = { for connector in values(local.tack_connectors) : "${connector.guest.node}/${connector.guest.vmid}" => connector.guest.hostname }
    lock_endpoints = merge(
      { vault = module.terrakube.lock_targets.lock_endpoints.vault },
      { for connector in values(local.tack_connectors) : connector.guest.node => { user = "root", address = connector.hypervisor.address } },
      { for connector in values(local.tack_connectors) : connector.guest.hostname => { user = "root", address = split("/", connector.guest.ipv6_cidr)[0] } },
    )
  }
}
output "lock_targets" { value = local.lock_targets }

output "managed_guests" { value = {} }
output "guest_locks" { value = [for connector in values(local.tack_connectors) : connector.guest.service_key] }
output "keyed_guest_locks" { value = {
  "module.tack_cloudflared" = { for environment, connector in local.tack_connectors : environment => connector.guest.service_key }
} }
