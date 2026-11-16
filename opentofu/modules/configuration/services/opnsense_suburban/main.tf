locals { management_ipv6 = "3d06:bad:b01:210::1" }

output "value" { value = {
  "hostname"        = "router.suburban.goodkind.io"
  "node"            = "suburban"
  "vmid"            = 201
  "kind"            = "qemu"
  "ipv4_vmnet"      = "10.240.4.1"
  "ipv6_vmnet"      = local.management_ipv6
  "ipv4_privileged" = "10.240.1.1"
  "ipv6_privileged" = "3d06:bad:b01:211::1"
  "ipv6"            = local.management_ipv6
  "ipv4_transit"    = "10.240.240.2"
  "ipv6_transit"    = "3d06:bad:b01:201::2"
  "transit_v4_net"  = "10.240.240.0/29"
  "transit_v6_net"  = "3d06:bad:b01:201::/64"
} }
