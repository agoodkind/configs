output "value" { value = {
  "hostname" = "isp-sonic.suburban.goodkind.io"
  "node"     = "suburban"
  "vmid"     = 906
  "routing_interfaces" = {
    "uplink" = {
      "name"        = "eth0"
      "network"     = "management"
      "mac_address" = "BC:24:11:52:06:00"
      "ipv4"        = "10.240.200.60"
      "ipv6"        = "3d06:bad:b01:200::60"
    }
    "outer" = {
      "name"        = "eth1"
      "network"     = "outer"
      "mac_address" = "BC:24:11:52:06:01"
      "ipv4"        = "198.51.100.2"
      "ipv6"        = ""
    }
    "sonic_1" = {
      "name"        = "eth2"
      "network"     = "sonic_1"
      "mac_address" = "BC:24:11:52:06:02"
      "ipv4"        = "10.240.209.1"
      "ipv6"        = ""
    }
    "sonic_2" = {
      "name"        = "eth3"
      "network"     = "sonic_2"
      "mac_address" = "BC:24:11:52:06:03"
      "ipv4"        = "10.240.210.1"
      "ipv6"        = ""
    }
  }
} }
