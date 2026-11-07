locals {
  frr_home_prefix   = "3d06:bad:b01:210::/60"
  frr_remote_prefix = "2001:db8:280:1::/64"
  frr_default_route = "::/0"

  # This address identifies the MWAN gateway on the etheric link, where it peers
  # with the provider router.
  frr_gateway_address = "2001:db8:2ff:20::2"

  frr_router_interfaces   = local.service_mapping.routing_etheric_native_router_suburban.routing_interfaces
  frr_upstream_interfaces = local.service_mapping.routing_etheric_native_upstream_suburban.routing_interfaces

  frr_session_timers = {
    keepalive_seconds = 3
    hold_seconds      = 9
    multihop_ttl      = 0
  }
}

output "frr" { value = {
  routing_etheric_native_router_suburban = {
    asn                                = 64520
    router_id                          = "192.0.2.9"
    graceful_restart_stalepath_seconds = 60
    networks                           = []
    tracked_routes                     = []
    peers = [
      merge(local.frr_session_timers, {
        id              = "etheric_native_upstream"
        address         = local.frr_upstream_interfaces.transit.ipv6
        asn             = 64521
        update_source   = local.frr_router_interfaces.transit.ipv6
        import_prefixes = [local.frr_remote_prefix, local.frr_default_route]
        export_prefixes = [local.frr_home_prefix]
      }),
      merge(local.frr_session_timers, {
        id              = "etheric_native_home"
        address         = local.frr_gateway_address
        asn             = 64512
        update_source   = local.frr_router_interfaces.provider.ipv6
        import_prefixes = [local.frr_home_prefix]
        export_prefixes = [local.frr_remote_prefix, local.frr_default_route]
      }),
    ]
  }
  routing_etheric_native_upstream_suburban = {
    asn                     = 64521
    router_id               = "192.0.2.10"
    default_route_interface = local.frr_upstream_interfaces.remote.name
    networks                = [local.frr_remote_prefix]
    tracked_routes          = []
    peers = [
      merge(local.frr_session_timers, {
        id              = "etheric_native_upstream"
        address         = local.frr_router_interfaces.transit.ipv6
        asn             = 64520
        update_source   = local.frr_upstream_interfaces.transit.ipv6
        import_prefixes = [local.frr_home_prefix]
        export_prefixes = [local.frr_remote_prefix, local.frr_default_route]
      }),
    ]
  }
} }
