locals {
  # These addresses identify guests outside this root and its service_mapping.
  network_gateway_astound_ipv4   = "10.240.207.2"
  network_isp_astound_outer_ipv4 = "198.51.100.1"
  network_gateway_inner_ipv6     = "2001:db8:1ff:1::1"
  network_home_prefix            = "3d06:bad:b01:210::/60"

  network_endpoint_interfaces = local.service_mapping.routing_astound_static_endpoint_suburban.routing_interfaces
}

output "network" { value = {
  routing_astound_static_endpoint_suburban = {
    role = "tunnel_endpoint"
    tunnels = [{
      interface           = "sit-astound"
      mtu                 = 1480
      outer_ttl           = 64
      inner_prefix_length = 64
      outer_ipv4          = local.network_endpoint_interfaces.outer.ipv4
      peer_outer_ipv4     = local.network_gateway_astound_ipv4
      inner_ipv6          = "2001:db8:1ff:1::2"
      peer_inner_ipv6     = local.network_gateway_inner_ipv6
    }]
    routes = [
      {
        destination = "${local.network_gateway_astound_ipv4}/32"
        gateway     = local.network_isp_astound_outer_ipv4
      },
      {
        destination = local.network_home_prefix
        gateway     = local.network_gateway_inner_ipv6
      },
    ]
  }
  routing_astound_static_client_suburban = {
    role = "remote_client"
    routes = [{
      destination = local.network_home_prefix
      gateway     = local.network_endpoint_interfaces.remote.ipv6
    }]
  }
} }
