# Device profiles run in Include mode, so a prefix reaches a device through WARP
# only when it appears in that profile's include list. The client computes the
# complement of the list and installs it as the excluded set, which is why an
# address that is simply absent still routes over the local link.
#
# Profiles are first match by precedence. The lower number wins.

locals {
  # Settings both profiles share. They differ only in name, match, precedence,
  # and include list.
  profile_defaults = {
    enabled                        = true
    allow_mode_switch              = true
    allow_updates                  = true
    allowed_to_leave               = true
    auto_connect                   = 0
    captive_portal                 = 180
    disable_auto_fallback          = false
    exclude_office_ips             = false
    lan_allow_minutes              = 120
    register_interface_ip_with_dns = true
    switch_locked                  = false
    tunnel_protocol                = "masque"
  }
}

# Applies while the device sits on the Berylax LAN. It omits that LAN's prefix,
# so the router answers over the local link rather than through the tunnel.
# Precedence 499 puts it ahead of the away profile.
resource "cloudflare_zero_trust_device_custom_profile" "berylax_lan" {
  account_id  = var.account_id
  name        = "tun-only on Berylax LAN"
  description = "Use direct LAN routing for 3d06:bad:b01:300::/64"
  match       = format("identity.email == %q and network == %q", var.owner_email, cloudflare_zero_trust_device_managed_networks.berylax_lan.name)
  precedence  = 499

  enabled                        = local.profile_defaults.enabled
  allow_mode_switch              = local.profile_defaults.allow_mode_switch
  allow_updates                  = local.profile_defaults.allow_updates
  allowed_to_leave               = local.profile_defaults.allowed_to_leave
  auto_connect                   = local.profile_defaults.auto_connect
  captive_portal                 = local.profile_defaults.captive_portal
  disable_auto_fallback          = local.profile_defaults.disable_auto_fallback
  exclude_office_ips             = local.profile_defaults.exclude_office_ips
  lan_allow_minutes              = local.profile_defaults.lan_allow_minutes
  register_interface_ip_with_dns = local.profile_defaults.register_interface_ip_with_dns
  switch_locked                  = local.profile_defaults.switch_locked
  tunnel_protocol                = local.profile_defaults.tunnel_protocol

  service_mode_v2 = {
    mode = "warp"
  }

  dns_search_suffixes = [
    { suffix = var.dns_search_suffix },
  ]

  include = var.berylax_include

  lifecycle {
    prevent_destroy = true
  }
}

# Applies everywhere else. It includes the Berylax prefix, so the router is
# reachable through the home-berylax connector while away.
resource "cloudflare_zero_trust_device_custom_profile" "tun_only" {
  account_id = var.account_id
  name       = "tun-only"
  match      = format("identity.email == %q", var.owner_email)
  precedence = 500

  enabled                        = local.profile_defaults.enabled
  allow_mode_switch              = local.profile_defaults.allow_mode_switch
  allow_updates                  = local.profile_defaults.allow_updates
  allowed_to_leave               = local.profile_defaults.allowed_to_leave
  auto_connect                   = local.profile_defaults.auto_connect
  captive_portal                 = local.profile_defaults.captive_portal
  disable_auto_fallback          = local.profile_defaults.disable_auto_fallback
  exclude_office_ips             = local.profile_defaults.exclude_office_ips
  lan_allow_minutes              = local.profile_defaults.lan_allow_minutes
  register_interface_ip_with_dns = local.profile_defaults.register_interface_ip_with_dns
  switch_locked                  = local.profile_defaults.switch_locked
  tunnel_protocol                = local.profile_defaults.tunnel_protocol

  service_mode_v2 = {
    mode = "warp"
  }

  dns_search_suffixes = [
    { suffix = var.dns_search_suffix },
  ]

  include = var.tun_only_include

  lifecycle {
    prevent_destroy = true
  }
}
