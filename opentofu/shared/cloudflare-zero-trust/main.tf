locals { repository_root = abspath("${path.module}/../../..") }

module "contracts" {
  source          = "../../modules/contracts"
  repository_root = local.repository_root
}

locals {
  shared_vars         = module.contracts.shared_vars
  service_mapping     = module.contracts.service_mapping
  cloudflare_zone_ids = module.contracts.cloudflare_zone_ids
}

module "cloudflare" {
  source = "../../modules/cloudflare"

  providers = {
    cloudflare         = cloudflare.zero_trust
    cloudflare.routes  = cloudflare.tunnel_routes
    cloudflare.tunnels = cloudflare.tunnels
  }

  account_id  = var.cloudflare_account_id
  owner_email = var.cloudflare_owner_email

  router_routes_enabled = var.cloudflare_router_routes_enabled

  berylax_beacon_sockaddr = var.berylax_beacon_sockaddr
  berylax_beacon_sha256   = var.berylax_beacon_sha256
  poweredge_beacon        = var.poweredge_beacon

  dns_search_suffix = "home.goodkind.io"

  # Both lists below are in the order Cloudflare stores them. The provider diffs
  # a split-tunnel list by position. Reordering an entry can make the provider
  # send one object with both address and host set, which the API rejects.
  #
  # The Berylax list still splits the site prefix into eight fragments that add up
  # to the /48 minus 3d06:bad:b01:300::/56. Rewriting it as explicit /56 entries
  # is a content change, so it stays out of this import.
  berylax_include = [
    { address = "3eef::/48", description = "suburban testbed v6" },
    { address = "3d06:bad:b01::/55", description = "v6-all except Berylax LAN /56" },
    { address = "3d06:bad:b01:800::/53", description = "v6-all except Berylax LAN /56" },
    { address = "3d06:bad:b01:8000::/49", description = "v6-all except Berylax LAN /56" },
    { address = "3d06:bad:b01:400::/54", description = "v6-all except Berylax LAN /56" },
    { address = "3d06:bad:b01:4000::/50", description = "v6-all except Berylax LAN /56" },
    { address = "3d06:bad:b01:200::/56", description = "v6-all except Berylax LAN /56" },
    { address = "3d06:bad:b01:2000::/51", description = "v6-all except Berylax LAN /56" },
    { address = "3d06:bad:b01:1000::/52", description = "v6-all except Berylax LAN /56" },
    { address = "100.96.0.0/12", description = "WARP device IPs" },
    { address = "10.250.0.0/16", description = "home-v4" },
    { address = "10.240.0.0/16", description = "suburban-v4" },
    { host = "*.cloudflareaccess.com" },
  ]

  tun_only_include = [
    { address = "3d06:bad:b01::/56", description = "home" },
    { address = "3d06:bad:b01:6400::/56", description = "production NAT64" },
    { address = "3d06:bad:b01:300::/56", description = "berylax" },
    { address = "3d06:bad:b01:2600::/56", description = "suburban NAT64" },
    { address = "3d06:bad:b01:2400::/56", description = "monkeybrains sim NPTv6 egress" },
    { address = "3d06:bad:b01:2300::/56", description = "att sim NPTv6 egress" },
    { address = "3d06:bad:b01:2200::/56", description = "webpass sim NPTv6 egress" },
    { address = "3d06:bad:b01:200::/56", description = "suburban" },
    { address = "100.96.0.0/12", description = "WARP device IPs" },
    { address = "10.250.0.0/16", description = "home-v4" },
    { address = "10.240.0.0/16", description = "suburban-v4" },
    { host = "*.cloudflareaccess.com" },
    { address = "10.230.0.0/24", description = "berylax-v4" },
  ]
}

output "cloudflare_router_tunnel_id" {
  description = "Cloudflare tunnel ID for the OPNsense connector."
  value       = module.cloudflare.router_tunnel_id
}

output "cloudflare_router_tunnel_token" {
  description = "Tunnel token for the OPNsense Cloudflared plugin."
  value       = module.cloudflare.router_tunnel_token
  sensitive   = true
}

output "poweredge_tunnel_ids" {
  description = "PowerEdge tunnel identifiers keyed by host or mwan."
  value       = module.cloudflare.poweredge_tunnel_ids
}
