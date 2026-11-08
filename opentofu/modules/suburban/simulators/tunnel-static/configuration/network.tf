locals {
  # These addresses identify guests outside this root and its service_mapping.
  network_gateway_sonic_1_ipv4 = "10.240.209.2"
  network_isp_sonic_outer_ipv4 = "198.51.100.2"

  network_home_tunnel_peer_inner_ipv6 = "2001:db8:3ff:1::1"

  route_tracker_interval_seconds  = 2
  route_tracker_failure_threshold = 3
  route_tracker_metric            = 100
}

output "route_trackers" { value = {
  routing_tunnel_static_vps_suburban = {
    destination       = local.frr_home_prefix
    gateway           = local.network_home_tunnel_peer_inner_ipv6
    interval_seconds  = local.route_tracker_interval_seconds
    failure_threshold = local.route_tracker_failure_threshold
    route_metric      = local.route_tracker_metric
  }
} }

output "network" { value = {
  routing_tunnel_static_vps_suburban = {
    role = "vps"
    tunnels = [{
      interface           = local.frr_home_tunnel_interface
      mtu                 = 1480
      outer_ttl           = 64
      inner_prefix_length = 64
      outer_ipv4          = local.frr_vps_interfaces.outer.ipv4
      peer_outer_ipv4     = local.network_gateway_sonic_1_ipv4
      inner_ipv6          = "2001:db8:3ff:1::2"
      peer_inner_ipv6     = local.network_home_tunnel_peer_inner_ipv6
    }]
    # The route tracker installs the home prefix route only while the tunnel
    # gateway answers probes. FRR announces that route through tracked_routes.
    routes = [{
      destination = "${local.network_gateway_sonic_1_ipv4}/32"
      gateway     = local.network_isp_sonic_outer_ipv4
    }]
  }
  routing_tunnel_static_upstream_suburban = { role = "upstream_router" }
  routing_tunnel_static_client_suburban = {
    role = "remote_client"
    routes = [{
      destination = local.frr_home_prefix
      gateway     = local.frr_upstream_interfaces.remote.ipv6
    }]
  }
} }
