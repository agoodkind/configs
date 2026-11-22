module "contracts" {
  source = "../../modules/contracts"
}

locals {
  management_routes = {
    ipv4 = "${split("/", var.management_ipv4_cidr)[0]}/32"
    ipv6 = "${split("/", var.management_ipv6_cidr)[0]}/128"
  }

  # Take the second half at each split, leaving out the first /24 and /64.
  # Keep the /17 and /57 in the original profile slots and append these entries.
  home_ipv4_include = [for bits in range(2, 9) : {
    address     = cidrsubnet("10.250.0.0/16", bits, 1)
    description = "home-v4"
  }]
  home_ipv6_include = [for bits in range(2, 9) : {
    address     = cidrsubnet("3d06:bad:b01::/56", bits, 1)
    description = "home"
  }]

  home_host_routes = [
    { address = "10.250.0.113/32", description = "home-v4" },
    { address = "3d06:bad:b01::254/128", description = "home" },
  ]
}

module "cloudflare" {
  source = "../../modules/cloudflare"

  providers = {
    cloudflare         = cloudflare.zero_trust
    cloudflare.routes  = cloudflare.tunnel_routes
    cloudflare.tunnels = cloudflare.tunnels
  }

  management_routes = local.management_routes
  account_id        = var.cloudflare_account_id
  owner_email       = var.cloudflare_owner_email

  berylax_beacon_sockaddr = var.berylax_beacon_sockaddr
  berylax_beacon_sha256   = var.berylax_beacon_sha256

  dns_search_suffix = "home.goodkind.io"

  # Keep the hostname at its existing position. The provider diffs by position
  # and can send both address and host in one entry if the hostname shifts.
  berylax_include = concat([
    { address = "3eef::/48", description = "suburban testbed v6" },
    { address = "3d06:bad:b01:80::/57", description = "home" },
    { address = "3d06:bad:b01:800::/53", description = "v6-all except Berylax LAN /56" },
    { address = "3d06:bad:b01:8000::/49", description = "v6-all except Berylax LAN /56" },
    { address = "3d06:bad:b01:400::/54", description = "v6-all except Berylax LAN /56" },
    { address = "3d06:bad:b01:4000::/50", description = "v6-all except Berylax LAN /56" },
    { address = "3d06:bad:b01:200::/56", description = "v6-all except Berylax LAN /56" },
    { address = "3d06:bad:b01:2000::/51", description = "v6-all except Berylax LAN /56" },
    { address = "3d06:bad:b01:1000::/52", description = "v6-all except Berylax LAN /56" },
    { address = "100.96.0.0/12", description = "WARP device IPs" },
    { address = "10.250.128.0/17", description = "home-v4" },
    { address = "10.240.0.0/16", description = "suburban-v4" },
    { host = "*.cloudflareaccess.com" },
    { address = "3d06:bad:b01:100::/56", description = "home" },
  ], local.home_ipv6_include, local.home_ipv4_include, local.home_host_routes)

  tun_only_include = concat([
    { address = "3d06:bad:b01:80::/57", description = "home" },
    { address = "3d06:bad:b01:6400::/56", description = "production NAT64" },
    { address = "3d06:bad:b01:2600::/56", description = "suburban NAT64" },
    { address = "3d06:bad:b01:2400::/56", description = "monkeybrains sim NPTv6 egress" },
    { address = "3d06:bad:b01:2300::/56", description = "att sim NPTv6 egress" },
    { address = "3d06:bad:b01:2200::/56", description = "webpass sim NPTv6 egress" },
    { address = "3d06:bad:b01:200::/56", description = "suburban" },
    { address = "100.96.0.0/12", description = "WARP device IPs" },
    { address = "10.250.128.0/17", description = "home-v4" },
    { address = "10.240.0.0/16", description = "suburban-v4" },
    { host = "*.cloudflareaccess.com" },
  ], local.home_ipv6_include, local.home_ipv4_include, local.home_host_routes)
}

module "configuration" { source = "../../modules/shared/cloudflare-zero-trust/configuration" }
