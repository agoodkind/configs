locals {
  # These addresses identify guests outside this root and its service_mapping.
  network_gateway_sonic_2_ipv4 = "10.240.210.2"
  network_isp_sonic_outer_ipv4 = "198.51.100.2"

  network_vps_interfaces = local.service_mapping.routing_tunnel_upstream_vps_suburban.routing_interfaces
  network_tunnel_prefix  = "2001:db8:4ff:1::/64"
}

output "network" { value = {
  routing_tunnel_upstream_vps_suburban = {
    role = "vps"
    tunnels = [{
      interface           = "sit-sonic2"
      mtu                 = 1480
      outer_ttl           = 64
      inner_prefix_length = 64
      outer_ipv4          = local.network_vps_interfaces.outer.ipv4
      peer_outer_ipv4     = local.network_gateway_sonic_2_ipv4
      inner_ipv6          = "2001:db8:4ff:1::2"
      peer_inner_ipv6     = local.frr_gateway_tunnel_address
    }]
    routes = [
      {
        destination = "${local.network_gateway_sonic_2_ipv4}/32"
        gateway     = local.network_isp_sonic_outer_ipv4
      },
      {
        destination = local.frr_home_prefix
        gateway     = local.frr_gateway_tunnel_address
      },
      {
        destination = local.frr_remote_prefix
        gateway     = local.frr_upstream_interfaces.transit.ipv6
      },
    ]
  }
  routing_tunnel_upstream_upstream_suburban = {
    role = "upstream_router"
    routes = [{
      destination = local.network_tunnel_prefix
      gateway     = local.network_vps_interfaces.transit.ipv6
    }]
  }
  routing_tunnel_upstream_client_suburban = {
    role = "remote_client"
    routes = [{
      destination = local.frr_home_prefix
      gateway     = local.frr_upstream_interfaces.remote.ipv6
    }]
  }
} }
