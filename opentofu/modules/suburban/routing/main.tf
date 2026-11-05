output "networks" { value = {
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
output "management" { value = {
  "forwarding"         = false
  "ipv4_default_route" = true
  "ipv6_default_route" = false
} }
