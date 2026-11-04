output "value" { value = {
  "hostname" = "routing-astound-static-endpoint.suburban.goodkind.io"
  "node"     = "suburban"
  "vmid"     = 907
  "routing_interfaces" = {
    "management" = {
      "name"        = "eth0"
      "network"     = "management"
      "mac_address" = "BC:24:11:52:07:00"
      "ipv4"        = "10.240.200.61"
      "ipv6"        = "3d06:bad:b01:200::61"
    }
    "outer" = {
      "name"        = "eth1"
      "network"     = "outer"
      "mac_address" = "BC:24:11:52:07:01"
      "ipv4"        = "198.51.100.10"
      "ipv6"        = ""
    }
    "remote" = {
      "name"        = "eth2"
      "network"     = "astound_static_remote"
      "mac_address" = "BC:24:11:52:07:02"
      "ipv4"        = ""
      "ipv6"        = "2001:db8:180:1::1"
    }
  }
} }
