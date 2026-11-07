locals {
  frr_home_prefix   = "3d06:bad:b01:210::/60"
  frr_remote_prefix = "2001:db8:580:1::/64"
  frr_default_route = "::/0"

  # The VPS opens one BGP session to the gateway per provider tunnel.
  # Each entry specifies the session ID and both tunnel addresses.
  frr_home_tunnels = [
    { id = "tunnel_vps_home_sonic_1", gateway = "2001:db8:5ff:1::1", vps = "2001:db8:5ff:1::2" },
    { id = "tunnel_vps_home_astound", gateway = "2001:db8:5ff:2::1", vps = "2001:db8:5ff:2::2" },
  ]

  frr_vps_interfaces      = local.service_mapping.routing_tunnel_vps_vps_suburban.routing_interfaces
  frr_upstream_interfaces = local.service_mapping.routing_tunnel_vps_upstream_suburban.routing_interfaces

  frr_session_timers = {
    keepalive_seconds = 3
    hold_seconds      = 9
    multihop_ttl      = 0
  }

  frr_vps_home_peers = [
    for tunnel in local.frr_home_tunnels : merge(local.frr_session_timers, {
      id              = tunnel.id
      address         = tunnel.gateway
      asn             = 64512
      update_source   = tunnel.vps
      import_prefixes = [local.frr_home_prefix]
      export_prefixes = [local.frr_remote_prefix, local.frr_default_route]
    })
  ]
}

output "frr" { value = {
  routing_tunnel_vps_vps_suburban = {
    asn                                = 64550
    router_id                          = "192.0.2.18"
    graceful_restart_stalepath_seconds = 60
    networks                           = []
    tracked_routes                     = []
    peers = concat([
      merge(local.frr_session_timers, {
        id              = "tunnel_vps_upstream"
        address         = local.frr_upstream_interfaces.transit.ipv6
        asn             = 64531
        update_source   = local.frr_vps_interfaces.transit.ipv6
        import_prefixes = [local.frr_remote_prefix, local.frr_default_route]
        export_prefixes = [local.frr_home_prefix]
      }),
    ], local.frr_vps_home_peers)
  }
  routing_tunnel_vps_upstream_suburban = {
    asn                     = 64531
    router_id               = "192.0.2.19"
    default_route_interface = local.frr_upstream_interfaces.remote.name
    networks                = [local.frr_remote_prefix]
    tracked_routes          = []
    peers = [
      merge(local.frr_session_timers, {
        id              = "tunnel_vps_upstream"
        address         = local.frr_vps_interfaces.transit.ipv6
        asn             = 64550
        update_source   = local.frr_upstream_interfaces.transit.ipv6
        import_prefixes = [local.frr_home_prefix]
        export_prefixes = [local.frr_remote_prefix, local.frr_default_route]
      }),
    ]
  }
} }
