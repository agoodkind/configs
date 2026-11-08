mock_provider "proxmox" {}

mock_provider "pveguest" {}

variables {
  guest_keys = ["routing_test_suburban"]
  ssh_keys   = "ssh-ed25519 AAAA test"
  routing_networks = {
    management = { bridge = "vmbr1", ipv4_net = "10.240.200.0/24", ipv6_net = "3d06:bad:b01:200::/64" }
    wide       = { bridge = "vmbr40", ipv4_net = "", ipv6_net = "2001:db8::/64" }
    narrow     = { bridge = "vmbr41", ipv4_net = "", ipv6_net = "2001:db8::/80" }
    other      = { bridge = "vmbr42", ipv4_net = "", ipv6_net = "2001:db8:2::/64" }
  }
  host_networks = { management = "vmbr1", wide = "vmbr40", narrow = "vmbr41", other = "vmbr42" }
  routing_management = {
    forwarding         = false
    ipv4_default_route = false
    ipv6_default_route = false
  }
  unreachable_prefixes = []
}

run "route_without_one_file_fails_before_apply" {
  command = plan

  variables {
    service_mapping = {
      routing_test_suburban = {
        hostname = "routing-test.suburban.goodkind.io"
        node     = "suburban"
        vmid     = 999
        routing_interfaces = {
          management = { name = "eth0", network = "management", mac_address = "BC:24:11:99:99:00", ipv4 = "10.240.200.99", ipv6 = "3d06:bad:b01:200::99" }
          wide       = { name = "eth1", network = "wide", mac_address = "BC:24:11:99:99:01", ipv4 = "", ipv6 = "2001:db8::10" }
          narrow     = { name = "eth2", network = "narrow", mac_address = "BC:24:11:99:99:02", ipv4 = "", ipv6 = "2001:db8::20" }
        }
      }
    }
    network = {
      routing_test_suburban = {
        role = "vps"
        routes = [
          { destination = "2001:db8:100::/64", gateway = "2001:db8::1" },
          { destination = "2001:db8:200::/64", gateway = "2001:db8:1::1" },
        ]
      }
    }
  }

  expect_failures = [pveguest_file.networkd_conf]
}

run "routes_with_one_file_each_plan" {
  command = plan

  variables {
    service_mapping = {
      routing_test_suburban = {
        hostname = "routing-test.suburban.goodkind.io"
        node     = "suburban"
        vmid     = 999
        routing_interfaces = {
          management = { name = "eth0", network = "management", mac_address = "BC:24:11:99:99:00", ipv4 = "10.240.200.99", ipv6 = "3d06:bad:b01:200::99" }
          wide       = { name = "eth1", network = "wide", mac_address = "BC:24:11:99:99:01", ipv4 = "", ipv6 = "2001:db8::10" }
          other      = { name = "eth2", network = "other", mac_address = "BC:24:11:99:99:02", ipv4 = "", ipv6 = "2001:db8:2::10" }
        }
      }
    }
    network = {
      routing_test_suburban = {
        role = "vps"
        routes = [
          { destination = "2001:db8:100::/64", gateway = "2001:db8::1" },
          { destination = "2001:db8:200::/64", gateway = "2001:db8:2::1" },
        ]
      }
    }
  }

  assert {
    condition = (
      strcontains(pveguest_file.interface_network["routing_test_suburban/eth1"].content, "Destination=2001:db8:100::/64\nGateway=2001:db8::1\n")
      && strcontains(pveguest_file.interface_network["routing_test_suburban/eth2"].content, "Destination=2001:db8:200::/64\nGateway=2001:db8:2::1\n")
    )
    error_message = "each interface file must contain the route with a gateway in its network"
  }
}
