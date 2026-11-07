locals {
  frr_home_prefix   = "3d06:bad:b01:210::/60"
  frr_remote_prefix = "2001:db8:380:1::/64"
  frr_default_route = "::/0"

  # FRR on the VPS redistributes the kernel route to the home prefix only when
  # the route uses this tunnel interface.
  frr_home_tunnel_interface = "sit-sonic1"

  frr_vps_interfaces      = local.service_mapping.routing_tunnel_static_vps_suburban.routing_interfaces
  frr_upstream_interfaces = local.service_mapping.routing_tunnel_static_upstream_suburban.routing_interfaces

  frr_session_timers = {
    keepalive_seconds = 3
    hold_seconds      = 9
    multihop_ttl      = 0
  }
}

output "frr" { value = {
  routing_tunnel_static_vps_suburban = {
    asn      = 64530
    networks = []
    tracked_routes = [{
      prefix    = local.frr_home_prefix
      interface = local.frr_home_tunnel_interface
    }]
    router_id = "192.0.2.12"
    peers = [
      merge(local.frr_session_timers, {
        id              = "tunnel_static_upstream"
        address         = local.frr_upstream_interfaces.transit.ipv6
        asn             = 64531
        update_source   = local.frr_vps_interfaces.transit.ipv6
        import_prefixes = [local.frr_remote_prefix, local.frr_default_route]
        export_prefixes = [local.frr_home_prefix]
      }),
    ]
  }
  routing_tunnel_static_upstream_suburban = {
    asn                     = 64531
    router_id               = "192.0.2.13"
    default_route_interface = local.frr_upstream_interfaces.remote.name
    networks                = [local.frr_remote_prefix]
    tracked_routes          = []
    peers = [
      merge(local.frr_session_timers, {
        id              = "tunnel_static_upstream"
        address         = local.frr_vps_interfaces.transit.ipv6
        asn             = 64530
        update_source   = local.frr_upstream_interfaces.transit.ipv6
        import_prefixes = [local.frr_home_prefix]
        export_prefixes = [local.frr_remote_prefix, local.frr_default_route]
      }),
    ]
  }
} }
