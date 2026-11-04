output "vars" { value = {
  "mwan_email_recipient" = "alerts@example.invalid"
  "mwan_host_config_embeds_secrets" = true
  "mwan_watchdog_description" = "MWAN watchdog fixture"
  "mwan_watchdog_enabled" = true
  "mwan_bgp_enabled" = "no"
  "mwan_ra_solicit" = "yes"
  "mwan_config_hostname" = "mwan-{{ mwan_vmid }}"
  "mwan_ping_targets" = ["2606:4700:4700::1111", "2001:4860:4860::8888"]
  "mwan_interfaces" = [{
    "name" = "vmbr0"
    "accept_ra" = 2
    "autoconf" = "yes"
  }, {
    "name" = "vmbr1"
    "accept_ra" = 0
    "autoconf" = false
  }]
} }
