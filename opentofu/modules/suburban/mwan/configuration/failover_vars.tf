output "failover_vars" { value = {
  "mwan_failover_ipv6_max_toggles_per_hour"                  = 0
  "mwan_failover_reconcile_interval"                         = "20s"
  "mwan_failover_connectivity_targets_v4"                    = ["1.1.1.1", "8.8.8.8"]
  "mwan_bgp_forwarding_readiness_socket_path"                = "/run/mwan-forwarding-ready.sock"
  "mwan_bgp_forwarding_readiness_poll_interval_milliseconds" = 1000
  "mwan_bgp_forwarding_readiness_read_timeout_milliseconds"  = 500
} }
