output "simulator" { value = {
  "ipv6_enabled"                         = true
  "dynamic_v4"                           = true
  "routed_ipv6_enabled"                  = false
  "routed_ipv6_prefix"                   = ""
  "routed_ipv4_prefix"                   = ""
  "routed_ipv4_route_to"                 = ""
  "static_v4_block"                      = "10.241.205.0/29"
  "static_v4_route_to"                   = "10.240.205.2"
  "v4_subnet"                            = "10.240.205.0/24"
  "pd_prefix"                            = "3d06:bad:b01:2300::/60"
  "slaac_prefix"                         = ""
  "mwan_vm_ll"                           = "fe80::be24:11ff:fec0:d760"
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
    "pool"                           = "10.240.205.100 - 10.240.205.200"
    "gateway"                        = "10.240.205.1"
    "dns"                            = "1.1.1.1"
    "reservations" = [{
      "mac_address" = "BC:24:11:C0:D7:60"
      "ipv4"        = "10.240.205.2"
    }]
  }
  "dhcp6" = {
    "preferred_lifetime_seconds"     = 3000
    "valid_lifetime_seconds"         = 4000
    "dns_servers"                    = []
    "socket_max_retries"             = 60
    "socket_retry_wait_milliseconds" = 5000
  }
} }
