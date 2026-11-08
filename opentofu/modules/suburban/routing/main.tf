locals { networks = {
  "management" = {
    "bridge"   = "vmbr1"
    "ipv4_net" = "10.240.200.0/24"
    "ipv6_net" = "3d06:bad:b01:200::/64"
  }
  "outer" = {
    "bridge"   = "vmbr20"
    "ipv4_net" = "198.51.100.0/24"
    "ipv6_net" = ""
  }
  "sonic_1" = {
    "bridge"   = "vmbr21"
    "ipv4_net" = "10.240.209.0/24"
    "ipv6_net" = ""
  }
  "sonic_2" = {
    "bridge"   = "vmbr22"
    "ipv4_net" = "10.240.210.0/24"
    "ipv6_net" = ""
  }
  "etheric" = {
    "bridge"   = "vmbr23"
    "ipv4_net" = "10.240.211.0/24"
    "ipv6_net" = "2001:db8:2ff:20::/64"
  }
  "astound_static_remote" = {
    "bridge"   = "vmbr24"
    "ipv4_net" = ""
    "ipv6_net" = "2001:db8:180:1::/64"
  }
  "etheric_native_transit" = {
    "bridge"   = "vmbr25"
    "ipv4_net" = ""
    "ipv6_net" = "2001:db8:2ff:10::/64"
  }
  "etheric_native_remote" = {
    "bridge"   = "vmbr26"
    "ipv4_net" = ""
    "ipv6_net" = "2001:db8:280:1::/64"
  }
  "tunnel_static_transit" = {
    "bridge"   = "vmbr27"
    "ipv4_net" = ""
    "ipv6_net" = "2001:db8:3ff:10::/64"
  }
  "tunnel_static_remote" = {
    "bridge"   = "vmbr28"
    "ipv4_net" = ""
    "ipv6_net" = "2001:db8:380:1::/64"
  }
  "tunnel_upstream_transit" = {
    "bridge"   = "vmbr29"
    "ipv4_net" = ""
    "ipv6_net" = "2001:db8:4ff:10::/64"
  }
  "tunnel_upstream_remote" = {
    "bridge"   = "vmbr30"
    "ipv4_net" = ""
    "ipv6_net" = "2001:db8:480:1::/64"
  }
  "tunnel_vps_transit" = {
    "bridge"   = "vmbr31"
    "ipv4_net" = ""
    "ipv6_net" = "2001:db8:5ff:10::/64"
  }
  "tunnel_vps_remote" = {
    "bridge"   = "vmbr32"
    "ipv4_net" = ""
    "ipv6_net" = "2001:db8:580:1::/64"
  }
} }
output "networks" { value = local.networks }

locals {
  home_prefix          = "3d06:bad:b01:210::/60"
  documentation_prefix = "2001:db8::/32"

  # Keep this order to match the existing guest files and preserve their hashes.
  unreachable_ipv4_networks = ["outer", "sonic_1", "sonic_2", "etheric"]
}

# Each guest installs unreachable routes or rules for these prefixes. IPv4 routes
# use metric 65000. IPv6 rules run after the main table lookup because zebra
# prefers a kernel route over a BGP route for the same prefix.
output "unreachable_prefixes" {
  value = concat(
    [local.home_prefix, local.documentation_prefix],
    [for name in local.unreachable_ipv4_networks : local.networks[name].ipv4_net],
  )

  precondition {
    condition = toset(local.unreachable_ipv4_networks) == toset([
      for name, network in local.networks : name
      if name != "management" && network.ipv4_net != ""
    ])
    error_message = "unreachable_ipv4_networks must list every network except management that has an ipv4_net value."
  }
}
output "management" { value = {
  "forwarding"         = false
  "ipv4_default_route" = true
  "ipv6_default_route" = false
} }
