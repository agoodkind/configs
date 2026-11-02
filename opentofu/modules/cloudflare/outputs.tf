output "managed_network_id" {
  description = "Identifier of the Berylax LAN managed network."
  value       = cloudflare_zero_trust_device_managed_networks.berylax_lan.id
}

output "device_profile_ids" {
  description = "Device profile identifiers, keyed by the name Cloudflare shows."
  value = {
    (cloudflare_zero_trust_device_custom_profile.berylax_lan.name) = cloudflare_zero_trust_device_custom_profile.berylax_lan.id
    (cloudflare_zero_trust_device_custom_profile.tun_only.name)    = cloudflare_zero_trust_device_custom_profile.tun_only.id
  }
}

output "beacon_sockaddr" {
  description = "Host and port the client probes to detect the Berylax LAN."
  value       = cloudflare_zero_trust_device_managed_networks.berylax_lan.config.tls_sockaddr
}
