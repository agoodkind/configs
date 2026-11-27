module "contracts" {
  source = "../../modules/contracts"
}

module "vault_network" {
  source = "../../modules/configuration/hosts/vault"
}

locals {
  # TODO: Replace fixed guest addresses with hostnames that k3s resolves
  # independently of the guest addresses.
  tack_static_route_label = "tack-tmp-statics"
  tack_static_guests = [
    "tack", "seaweedfs", "tack_data1", "tack_data2", "tack_data3",
    "tack_k3s", "tack_search1",
  ]
  tack_static_routes = contains(var.tack_k3s_environments, "prod") ? flatten([
    for guest in local.tack_static_guests : [
      for cidr in module.vault_network.guest_networks[guest].addresses :
      "${split("/", cidr)[0]}/${strcontains(cidr, ":") ? 128 : 32}"
    ]
  ]) : []

  warp_include = concat([
    { address = "3d06:bad:b01:200::/56", description = "Suburban IPv6" },
    { address = "10.240.0.0/16", description = "Suburban IPv4" },
    { address = "3d06:bad:b01:6400::/56", description = "Production NAT64" },
    { address = "100.96.0.0/12", description = "WARP device addresses" },
    ], [
    for route in local.tack_static_routes : {
      address     = route
      description = local.tack_static_route_label
    }
  ], [{ host = "*.cloudflareaccess.com" }])
}

module "cloudflare" {
  source = "../../modules/cloudflare"

  providers = {
    cloudflare         = cloudflare.zero_trust
    cloudflare.tunnels = cloudflare.tunnels
  }

  account_id  = var.cloudflare_account_id
  owner_email = var.cloudflare_owner_email

  berylax_beacon_sockaddr = var.berylax_beacon_sockaddr
  berylax_beacon_sha256   = var.berylax_beacon_sha256

  warp_include = local.warp_include
}

module "configuration" { source = "../../modules/shared/cloudflare-zero-trust/configuration" }
