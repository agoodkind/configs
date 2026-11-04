output "value" { value = {
  "hostname" = "routing-tunnel-upstream-vps.suburban.goodkind.io"
  "node"     = "suburban"
  "vmid"     = 915
  "routing_interfaces" = {
    "management" = {
      "name"        = "eth0"
      "network"     = "management"
      "mac_address" = "BC:24:11:52:15:00"
      "ipv4"        = "10.240.200.69"
      "ipv6"        = "3d06:bad:b01:200::69"
    }
    "outer" = {
      "name"        = "eth1"
      "network"     = "outer"
      "mac_address" = "BC:24:11:52:15:01"
      "ipv4"        = "198.51.100.30"
      "ipv6"        = ""
    }
    "transit" = {
      "name"        = "eth2"
      "network"     = "tunnel_upstream_transit"
      "mac_address" = "BC:24:11:52:15:02"
      "ipv4"        = ""
      "ipv6"        = "2001:db8:4ff:10::1"
    }
  }
} }
