output "simulator" { value = {
  "ipv6_enabled"                         = true
  "dynamic_v4"                           = true
  "routed_ipv6_enabled"                  = false
  "routed_ipv6_prefix"                   = ""
  "routed_ipv4_prefix"                   = ""
  "routed_ipv4_route_to"                 = ""
  "static_v4_block"                      = ""
  "static_v4_route_to"                   = ""
  "v4_subnet"                            = "10.240.206.0/24"
  "pd_prefix"                            = "3d06:bad:b01:2400::/56"
  "slaac_prefix"                         = "3d06:bad:b01:250::/64"
  "mwan_vm_ll"                           = "fe80::be24:11ff:fe3d:cecc"
  "ra_min_interval_seconds"              = 30
  "ra_max_interval_seconds"              = 100
  "ra_dns_servers"                       = []
  "ra_dns_lifetime_seconds"              = "omit"
  "ra_prefix_preferred_lifetime_seconds" = "omit"
  "ra_prefix_valid_lifetime_seconds"     = "omit"
  "dhcp4" = {
    "valid_lifetime_seconds"         = 4000
    "socket_max_retries"             = 60
    "socket_retry_wait_milliseconds" = 1000
    "pool"                           = "10.240.206.100 - 10.240.206.200"
    "gateway"                        = "10.240.206.1"
    "dns"                            = "1.1.1.1"
    "reservations"                   = []
  }
  "dhcp6" = {
    "preferred_lifetime_seconds"     = 3000
    "valid_lifetime_seconds"         = 4000
    "dns_servers"                    = []
    "socket_max_retries"             = 60
    "socket_retry_wait_milliseconds" = 5000
    "address_pool"                   = "3d06:bad:b01:250::1000 - 3d06:bad:b01:250::1fff"
  }
} }
