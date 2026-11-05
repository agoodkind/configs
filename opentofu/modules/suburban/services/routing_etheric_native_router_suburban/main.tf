output "value" { value = {
  "hostname" = "routing-etheric-native-router.suburban.goodkind.io"
  "node"     = "suburban"
  "vmid"     = 909
  "routing_interfaces" = {
    "management" = {
      "name"        = "eth0"
      "network"     = "management"
      "mac_address" = "BC:24:11:52:09:00"
      "ipv4"        = "10.240.200.63"
      "ipv6"        = "3d06:bad:b01:200::63"
    }
    "provider" = {
      "name"        = "eth1"
      "network"     = "etheric"
      "mac_address" = "BC:24:11:52:09:01"
      "ipv4"        = "10.240.211.1"
      "ipv6"        = "2001:db8:2ff:20::1"
    }
    "transit" = {
      "name"        = "eth2"
      "network"     = "etheric_native_transit"
      "mac_address" = "BC:24:11:52:09:02"
      "ipv4"        = ""
      "ipv6"        = "2001:db8:2ff:10::1"
    }
  }
} }
