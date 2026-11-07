locals {
  frr_home_prefix   = "3d06:bad:b01:210::/60"
  frr_remote_prefix = "2001:db8:480:1::/64"
  frr_default_route = "::/0"

  # The upstream router peers with the gateway tunnel address two hops away
  # through the VPS, using ebgp-multihop 2.
  frr_gateway_tunnel_address = "2001:db8:4ff:1::1"
  frr_gateway_multihop_ttl   = 2

  frr_upstream_interfaces = local.service_mapping.routing_tunnel_upstream_upstream_suburban.routing_interfaces
}

output "frr" { value = {
  routing_tunnel_upstream_upstream_suburban = {
    asn                                = 64541
    router_id                          = "192.0.2.16"
    graceful_restart_stalepath_seconds = 60
    default_route_interface            = local.frr_upstream_interfaces.remote.name
    networks                           = [local.frr_remote_prefix]
    tracked_routes                     = []
    peers = [{
      id                = "tunnel_upstream_home"
      address           = local.frr_gateway_tunnel_address
      asn               = 64512
      update_source     = local.frr_upstream_interfaces.transit.ipv6
      keepalive_seconds = 3
      hold_seconds      = 9
      multihop_ttl      = local.frr_gateway_multihop_ttl
      import_prefixes   = [local.frr_home_prefix]
      export_prefixes   = [local.frr_remote_prefix, local.frr_default_route]
    }]
  }
} }
