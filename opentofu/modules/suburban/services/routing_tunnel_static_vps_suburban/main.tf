output "value" { value = {
  "hostname" = "routing-tunnel-static-vps.suburban.goodkind.io"
  "node"     = "suburban"
  "vmid"     = 912
  "routing_interfaces" = {
    "management" = {
      "name"        = "eth0"
      "network"     = "management"
      "mac_address" = "BC:24:11:52:12:00"
      "ipv4"        = "10.240.200.66"
      "ipv6"        = "3d06:bad:b01:200::66"
    }
    "outer" = {
      "name"        = "eth1"
      "network"     = "outer"
      "mac_address" = "BC:24:11:52:12:01"
      "ipv4"        = "198.51.100.20"
      "ipv6"        = ""
    }
    "transit" = {
      "name"        = "eth2"
      "network"     = "tunnel_static_transit"
      "mac_address" = "BC:24:11:52:12:02"
      "ipv4"        = ""
      "ipv6"        = "2001:db8:3ff:10::1"
    }
  }
} }
