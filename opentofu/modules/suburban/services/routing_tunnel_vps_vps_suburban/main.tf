output "value" { value = {
  "hostname" = "routing-tunnel-vps-vps.suburban.goodkind.io"
  "node"     = "suburban"
  "vmid"     = 918
  "routing_interfaces" = {
    "management" = {
      "name"        = "eth0"
      "network"     = "management"
      "mac_address" = "BC:24:11:52:18:00"
      "ipv4"        = "10.240.200.72"
      "ipv6"        = "3d06:bad:b01:200::72"
    }
    "outer" = {
      "name"        = "eth1"
      "network"     = "outer"
      "mac_address" = "BC:24:11:52:18:01"
      "ipv4"        = "198.51.100.40"
      "ipv6"        = ""
    }
    "transit" = {
      "name"        = "eth2"
      "network"     = "tunnel_vps_transit"
      "mac_address" = "BC:24:11:52:18:02"
      "ipv4"        = ""
      "ipv6"        = "2001:db8:5ff:10::1"
    }
  }
} }
