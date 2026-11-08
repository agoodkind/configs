locals {
  # These addresses identify guests outside this root and its service_mapping.
  network_gateway_sonic_1_ipv4   = "10.240.209.2"
  network_gateway_astound_ipv4   = "10.240.207.2"
  network_isp_sonic_outer_ipv4   = "198.51.100.2"
  network_isp_astound_outer_ipv4 = "198.51.100.1"

  # Each entry matches the entry at the same index in frr_home_tunnels.
  network_home_tunnel_links = [
    { interface = "sit-sonic1", peer_outer_ipv4 = local.network_gateway_sonic_1_ipv4 },
    { interface = "sit-astound", peer_outer_ipv4 = local.network_gateway_astound_ipv4 },
  ]
}

output "network" { value = {
  routing_tunnel_vps_vps_suburban = {
    role = "vps"
    tunnels = [
      for index, tunnel in local.frr_home_tunnels : {
        interface           = local.network_home_tunnel_links[index].interface
        mtu                 = 1480
        outer_ttl           = 64
        inner_prefix_length = 64
        outer_ipv4          = local.frr_vps_interfaces.outer.ipv4
        peer_outer_ipv4     = local.network_home_tunnel_links[index].peer_outer_ipv4
        inner_ipv6          = tunnel.vps
        peer_inner_ipv6     = tunnel.gateway
      }
    ]
    routes = [
      {
        destination = "${local.network_gateway_sonic_1_ipv4}/32"
        gateway     = local.network_isp_sonic_outer_ipv4
      },
      {
        destination = "${local.network_gateway_astound_ipv4}/32"
        gateway     = local.network_isp_astound_outer_ipv4
      },
    ]
  }
  routing_tunnel_vps_upstream_suburban = { role = "upstream_router" }
  routing_tunnel_vps_client_suburban = {
    role = "remote_client"
    routes = [{
      destination = local.frr_home_prefix
      gateway     = local.frr_upstream_interfaces.remote.ipv6
    }]
  }
} }
