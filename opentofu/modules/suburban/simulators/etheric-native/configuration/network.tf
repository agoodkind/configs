output "network" { value = {
  routing_etheric_native_router_suburban   = { role = "provider_router" }
  routing_etheric_native_upstream_suburban = { role = "upstream_router" }
  routing_etheric_native_client_suburban = {
    role = "remote_client"
    routes = [{
      destination = local.frr_home_prefix
      gateway     = local.frr_upstream_interfaces.remote.ipv6
    }]
  }
} }
