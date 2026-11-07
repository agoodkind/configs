output "simulator" { value = {
  "ipv6_enabled"                         = false
  "dynamic_v4"                           = true
  "routed_ipv6_enabled"                  = false
  "routed_ipv6_prefix"                   = ""
  "routed_ipv4_prefix"                   = ""
  "routed_ipv4_route_to"                 = ""
  "static_v4_block"                      = ""
  "static_v4_route_to"                   = ""
  "v4_subnet"                            = "10.240.207.0/24"
  "pd_prefix"                            = ""
  "slaac_prefix"                         = ""
  "mwan_vm_ll"                           = ""
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
    "pool"                           = "10.240.207.100 - 10.240.207.200"
    "gateway"                        = "10.240.207.1"
    "dns"                            = "1.1.1.1"
    "reservations" = [{
      "mac_address" = "BC:24:11:A5:70:06"
      "ipv4"        = "10.240.207.2"
    }]
  }
} }
