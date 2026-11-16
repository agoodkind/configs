output "value" { value = {
  "hostname" = "routing-tunnel-upstream-upstream.suburban.goodkind.io"
  "node"     = "suburban"
  "vmid"     = 916
  "routing_interfaces" = {
    "management" = {
      "name"        = "eth0"
      "network"     = "management"
      "mac_address" = "BC:24:11:52:16:00"
      "ipv4"        = "10.240.200.70"
      "ipv6"        = "3d06:bad:b01:200::70"
    }
    "transit" = {
      "name"        = "eth1"
      "network"     = "tunnel_upstream_transit"
      "mac_address" = "BC:24:11:52:16:01"
      "ipv4"        = ""
      "ipv6"        = "2001:db8:4ff:10::2"
    }
    "remote" = {
      "name"        = "eth2"
      "network"     = "tunnel_upstream_remote"
      "mac_address" = "BC:24:11:52:16:02"
      "ipv4"        = ""
      "ipv6"        = "2001:db8:480:1::1"
    }
  }
} }
