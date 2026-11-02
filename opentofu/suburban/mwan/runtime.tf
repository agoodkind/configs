# Gateway and failover MWAN runtime for the Suburban testbed.
#
# Each local below ports a value from the Ansible inventory that the Ansible
# playbooks read today. The comment above each group lists the source file. Guest
# identifiers and addresses come from the service mapping.

locals {
  group_vars_directory = "${local.repository_root}/ansible/inventory/group_vars"

  # ansible/inventory/group_vars/mwan_testbed_all.yml pins the release.
  testbed_release = yamldecode(file("${local.group_vars_directory}/mwan_testbed_all.yml"))
  mwan_release    = local.testbed_release.mwan_release_tag
  mwan_arch       = "amd64"

  gateway_map  = local.service_mapping.mwan_suburban
  failover_map = local.service_mapping.mwan_failover_suburban
  router_map   = local.service_mapping.opnsense_suburban

  routing_networks = module.contracts.routing_inventory.testbed_routing_networks

  gateway_macs = module.suburban.mwan_suburban_mac_addresses

  # ansible/inventory/group_vars/all/vars.yml
  bgp_keepalive_seconds = 10
  bgp_hold_seconds      = 30
  status_push_port      = 50053
  reserved_tables       = [400, 500]
  restconf_port         = 10080
  resolver_state_file   = "/var/lib/mwan/owned-resolver.json"
  pve_token_id          = "root@pam!watchdog"

  # ansible/inventory/group_vars/testbed_routing_all/prefixes.yml
  routing_prefixes = yamldecode(file("${local.group_vars_directory}/testbed_routing_all/prefixes.yml"))
  home_prefix      = local.routing_prefixes.testbed_routing_home_prefix

  # ansible/inventory/group_vars/mwan_suburban_servers.yml. The decoder reads the
  # plain values and leaves the template strings unread.
  suburban_vars   = yamldecode(file("${local.group_vars_directory}/mwan_suburban_servers.yml"))
  internal_prefix = local.suburban_vars.mwan_internal_prefix
  suburban_providers = {
    for provider in local.suburban_vars.mwan_providers : provider.name => provider
  }

  # Link prefix sizes of the routed simulator network, from mwan_routed_ipv4 and
  # the routed provider entry in mwan_suburban_servers.yml.
  routed_ipv4_prefix_size = 24
  routed_ipv6_prefix_size = 64

  # Roles and units of the two guests.
  gateway_role     = "wan"
  failover_role    = "failover"
  excluded_units   = toset([])
  bgp_asn          = 4200000001
  bgp_listen_port  = 179
  bgp_announce_v4  = ["0.0.0.0/0"]
  bgp_announce_v6  = ["::/0"]
  transit_networks = [local.router_map.transit_v4_net, local.router_map.transit_v6_net]

  alert_recipient = "alex@goodkind.io"
  alert_level     = "ERROR"
  alert_cooldown  = "5m"
}

# ansible/inventory/group_vars/mwan_suburban_servers.yml and
# mwan/config/sysctl-mwan.conf.j2 supply the gateway sysctl values. The interface
# names come from the same group variables.
locals {
  internal_interface = "enmwanbr0"
  management_iface   = "enmgmt0"
  att_interface      = "enatt0"
  webpass_interface  = "enwebpass0"
  mbrains_interface  = "enmbrains0"
  astound_interface  = "enastound0"
  routed_interface   = "enrouted0"
  sonic_1_interface  = "ensonic1"
  sonic_2_interface  = "ensonic2"
  etheric_interface  = "enetheric0"

  gateway_sysctl = {
    "net.ipv4.ip_forward"                                 = "1"
    "net.ipv6.conf.all.forwarding"                        = "1"
    "net.ipv6.conf.default.forwarding"                    = "1"
    "net.ipv6.conf.all.accept_ra"                         = "0"
    "net.ipv6.conf.default.accept_ra"                     = "0"
    "net.ipv6.conf.${local.internal_interface}.accept_ra" = "2"
    "net.netfilter.nf_conntrack_max"                      = "262144"
    "net.ipv4.conf.all.rp_filter"                         = "2"
    "net.ipv4.conf.default.rp_filter"                     = "2"
    "net.ipv4.conf.${local.webpass_interface}.rp_filter"  = "0"
    "net.ipv4.conf.${local.att_interface}.rp_filter"      = "0"
    "net.ipv6.conf.all.use_tempaddr"                      = "0"
    "net.ipv6.conf.default.use_tempaddr"                  = "0"
    "net.ipv4.tcp_fastopen"                               = "3"
    "net.core.rmem_max"                                   = "134217728"
    "net.core.wmem_max"                                   = "134217728"
    "net.ipv4.tcp_rmem"                                   = "4096 87380 67108864"
    "net.ipv4.tcp_wmem"                                   = "4096 65536 67108864"
    "net.ipv4.tcp_ecn"                                    = "1"
    "net.ipv4.tcp_syncookies"                             = "1"
    "net.ipv4.tcp_max_syn_backlog"                        = "8192"
  }

  # mwan-failover/sysctl.conf
  failover_sysctl = {
    "net.ipv6.conf.all.forwarding" = "1"
    "net.ipv6.conf.eth0.accept_ra" = "2"
    "net.ipv4.ip_forward"          = "1"
  }
}

# Provider constants from ansible/inventory/group_vars/mwan_suburban_servers.yml.
locals {
  health_targets_v4 = ["1.1.1.1", "8.8.8.8"]
  health_targets_v6 = ["2606:4700:4700::1111", "2001:4860:4860::8888"]
  health_http_urls  = ["https://ifconfig.co/ip"]

  # The five transit hosts that every static mapping lists, in the order of
  # mwan_static_internal_hosts.
  static_internal_hosts = [
    local.router_map.ipv4_transit,
    local.gateway_map.ipv4_transit,
    local.failover_map.ipv4_transit,
    cidrhost(local.router_map.transit_v4_net, 5),
    cidrhost(local.router_map.transit_v4_net, 6),
  ]

  # The external addresses come from the static mappings of the provider
  # entries in mwan_suburban_servers.yml.
  att_mappings = [
    for index, host in local.static_internal_hosts : {
      external = local.suburban_providers.att.ipv4.translation.static_mappings[index].external
      internal = host
    }
  ]

  webpass_mappings = [
    for index, host in local.static_internal_hosts : {
      external = local.suburban_providers.webpass.ipv4.translation.static_mappings[index].external
      internal = host
      delivery = "local"
    }
  ]

  isp_webpass = local.service_mapping.isp_webpass_suburban
  isp_routed  = local.service_mapping.isp_routed_suburban
  isp_sonic   = local.service_mapping.isp_sonic_suburban
  etheric_sim = local.service_mapping.routing_etheric_native_router_suburban

  vps_sim      = local.service_mapping.routing_tunnel_vps_vps_suburban
  upstream_sim = local.service_mapping.routing_tunnel_upstream_vps_suburban
  static_sim   = local.service_mapping.routing_tunnel_static_vps_suburban
  astound_sim  = local.service_mapping.routing_astound_static_endpoint_suburban

  # ansible/inventory/group_vars/testbed_routing_all/connections.yml
  mwan_sonic_1_ipv4 = local.gateway_map.routing_interfaces.sonic_1.ipv4
  mwan_sonic_2_ipv4 = local.gateway_map.routing_interfaces.sonic_2.ipv4
  mwan_etheric_ipv4 = local.gateway_map.routing_interfaces.etheric.ipv4
  mwan_etheric_ipv6 = local.gateway_map.routing_interfaces.etheric.ipv6
  mwan_astound_ipv4 = local.gateway_map.ipv4_astound
  sonic_1_link_ipv4 = local.isp_sonic.routing_interfaces.sonic_1.ipv4
  sonic_2_link_ipv4 = local.isp_sonic.routing_interfaces.sonic_2.ipv4
  etheric_link_ipv4 = local.etheric_sim.routing_interfaces.provider.ipv4
  etheric_link_ipv6 = local.etheric_sim.routing_interfaces.provider.ipv6

  import_prefix_all = { prefix = "::/0", "min-length" = 0, "max-length" = 0 }

  standard_health = {
    enabled              = true
    "ping-count"         = 3
    "success-threshold"  = 2
    "failure-threshold"  = 2
    "recovery-threshold" = 2
    "check-interval"     = 10
    "targets-v4"         = local.health_targets_v4
    "targets-v6"         = local.health_targets_v6
    "http-urls"          = local.health_http_urls
  }

  # The tunnel and underlay providers share this health shape. The targets differ.
  link_health = {
    enabled              = true
    "ping-count"         = 3
    "success-threshold"  = 1
    "failure-threshold"  = 2
    "recovery-threshold" = 2
    "check-interval"     = 10
  }
}

# One entry per provider, in the order of mwan_providers. Each entry is the
# object that mwan/config/network.json.j2 builds for that provider.
locals {
  nptv6_translation_att = {
    mode = "ietf-nat:nptv6"
    nptv6 = {
      "internal-prefix" = local.internal_prefix
      "external-source" = "delegated"
      "expected-prefix" = local.suburban_providers.att.ipv6.translation.nptv6.expected_prefix
    }
  }

  nptv6_translation_webpass = {
    mode = "ietf-nat:nptv6"
    nptv6 = {
      "internal-prefix" = local.internal_prefix
      "external-source" = "delegated"
      "expected-prefix" = local.suburban_providers.webpass.ipv6.translation.nptv6.expected_prefix
    }
  }

  provider_att = {
    name                                   = local.att_interface
    type                                   = "iana-if-type:other"
    "goodkind-mwan-steering:connection-id" = "att"
    "goodkind-mwan-steering:owner"         = "networkd"
    "goodkind-mwan-steering:link-files"    = "hand-authored"
    "goodkind-mwan-steering:wan" = {
      name           = "att"
      "table-id"     = 100
      "fw-mark"      = 1
      "fw-mark-prio" = 100
      "from-prio"    = 55
      "forced-dscp"  = 8
      health         = local.standard_health
    }
    "goodkind-mwan-steering:steering" = { tier = 0, weight = 1 }
    "ietf-ip:ipv4" = {
      "goodkind-mwan-steering:translation" = {
        mode             = "ietf-nat:napt44"
        "static-mapping" = local.att_mappings
      }
    }
    "ietf-ip:ipv6" = {
      "goodkind-mwan-steering:translation" = local.nptv6_translation_att
      "goodkind-mwan-steering:dhcp"        = true
      "goodkind-mwan-steering:delegation" = {
        hint                   = "::/60"
        "use-delegated-prefix" = true
        "without-ra"           = "solicit"
      }
    }
  }

  provider_webpass = {
    name                                   = local.webpass_interface
    type                                   = "iana-if-type:other"
    "goodkind-mwan-steering:connection-id" = "webpass"
    "goodkind-mwan-steering:owner"         = "mwan"
    "goodkind-mwan-steering:link" = {
      match = { "hardware-address" = lower(local.gateway_macs.webpass) }
    }
    "goodkind-mwan-steering:wan" = {
      name           = "webpass"
      "table-id"     = 200
      "fw-mark"      = 2
      "fw-mark-prio" = 200
      "from-prio"    = 56
      health         = local.standard_health
    }
    "goodkind-mwan-steering:steering" = { tier = 0, weight = 1 }
    "ietf-ip:ipv4" = {
      "goodkind-mwan-steering:translation" = {
        mode             = "ietf-nat:napt44"
        "static-mapping" = local.webpass_mappings
      }
      forwarding                            = true
      "goodkind-mwan-steering:dhcp"         = false
      "goodkind-mwan-steering:gateway"      = local.isp_webpass.ipv4
      "goodkind-mwan-steering:route-metric" = 10
      address                               = [{ ip = local.gateway_map.ipv4_webpass, "prefix-length" = local.isp_webpass.ipv4_prefix }]
    }
    "ietf-ip:ipv6" = {
      "goodkind-mwan-steering:translation" = local.nptv6_translation_webpass
      forwarding                           = true
      "goodkind-mwan-steering:dhcp"        = true
      "goodkind-mwan-steering:accept-ra"   = true
      "goodkind-mwan-steering:delegation" = {
        hint         = "::/56"
        iaid         = 1370549548
        "duid-type"  = "vendor"
        duid         = "00:02:00:00:ab:11:11:18:28:f5:2b:50:d8:af"
        "without-ra" = "solicit"
      }
    }
  }

  provider_tertiary = {
    name                                   = local.mbrains_interface
    type                                   = "iana-if-type:other"
    "goodkind-mwan-steering:connection-id" = "tertiary"
    "goodkind-mwan-steering:owner"         = "mwan"
    "goodkind-mwan-steering:link" = {
      match = { "hardware-address" = lower(local.gateway_macs.monkeybrains) }
    }
    "goodkind-mwan-steering:wan" = {
      name           = "tertiary"
      "table-id"     = 300
      "fw-mark"      = 3
      "fw-mark-prio" = 300
      "from-prio"    = 57
      health = {
        enabled              = true
        "ping-count"         = 5
        "success-threshold"  = 1
        "failure-threshold"  = 5
        "recovery-threshold" = 3
        "check-interval"     = 30
        "targets-v4"         = local.health_targets_v4
        "targets-v6"         = local.health_targets_v6
        "http-urls"          = local.health_http_urls
      }
    }
    "goodkind-mwan-steering:steering" = { tier = 1, weight = 1 }
    "ietf-ip:ipv4" = {
      "goodkind-mwan-steering:translation"  = { mode = "ietf-nat:napt44" }
      forwarding                            = true
      "goodkind-mwan-steering:dhcp"         = true
      "goodkind-mwan-steering:route-metric" = 5000
    }
    "ietf-ip:ipv6" = {
      "goodkind-mwan-steering:translation"  = { mode = "masquerade" }
      forwarding                            = true
      "goodkind-mwan-steering:dhcp"         = true
      "goodkind-mwan-steering:accept-ra"    = true
      "goodkind-mwan-steering:route-metric" = 5000
      "goodkind-mwan-steering:autoconf"     = true
      "goodkind-mwan-steering:dhcpv6-client" = {
        duid              = "00:03:00:01:${lower(local.gateway_macs.monkeybrains)}"
        "address-iaid"    = 714156292
        "request-address" = true
        "request-prefix"  = false
        "without-ra"      = "solicit"
      }
    }
  }

  provider_routed = {
    name                                   = local.routed_interface
    type                                   = "iana-if-type:other"
    "goodkind-mwan-steering:connection-id" = "routed"
    "goodkind-mwan-steering:owner"         = "mwan"
    "goodkind-mwan-steering:link" = {
      match = { "hardware-address" = lower(local.gateway_macs.routed) }
    }
    "goodkind-mwan-steering:wan" = {
      name           = "routed"
      "table-id"     = 600
      "fw-mark"      = 4
      "fw-mark-prio" = 400
      "from-prio"    = 58
      health = merge(local.link_health, {
        "targets-v4" = [local.isp_routed.ipv4]
        "targets-v6" = [local.isp_routed.ipv6]
        "http-urls"  = local.health_http_urls
      })
    }
    "goodkind-mwan-steering:steering" = { tier = 2, weight = 1 }
    "ietf-ip:ipv4" = {
      "goodkind-mwan-steering:translation"  = { mode = "native" }
      forwarding                            = true
      "goodkind-mwan-steering:dhcp"         = false
      "goodkind-mwan-steering:gateway"      = local.isp_routed.ipv4
      "goodkind-mwan-steering:route-metric" = 6000
      address                               = [{ ip = local.gateway_map.ipv4_routed, "prefix-length" = local.routed_ipv4_prefix_size }]
    }
    "ietf-ip:ipv6" = {
      "goodkind-mwan-steering:translation"  = { mode = "native" }
      forwarding                            = true
      "goodkind-mwan-steering:gateway"      = local.isp_routed.ipv6
      "goodkind-mwan-steering:route-metric" = 6000
      "goodkind-mwan-steering:accept-ra"    = false
      address                               = [{ ip = local.gateway_map.ipv6_routed, "prefix-length" = local.routed_ipv6_prefix_size }]
    }
  }

  provider_astound = {
    name                                   = local.astound_interface
    type                                   = "iana-if-type:other"
    "goodkind-mwan-steering:connection-id" = "astound"
    "goodkind-mwan-steering:owner"         = "mwan"
    "goodkind-mwan-steering:link" = {
      match = { "hardware-address" = lower(local.gateway_macs.astound) }
    }
    "goodkind-mwan-steering:wan" = {
      name           = "astound"
      "table-id"     = 700
      "fw-mark"      = 5
      "fw-mark-prio" = 500
      "from-prio"    = 59
      "forced-dscp"  = 31
      health         = merge(local.standard_health, { "targets-v6" = [] })
    }
    "goodkind-mwan-steering:steering" = { tier = 1, weight = 1 }
    "ietf-ip:ipv4" = {
      "goodkind-mwan-steering:translation"  = { mode = "ietf-nat:napt44" }
      forwarding                            = true
      "goodkind-mwan-steering:dhcp"         = true
      "goodkind-mwan-steering:route-metric" = 7000
    }
    "ietf-ip:ipv6" = {
      "goodkind-mwan-steering:translation" = { mode = "native" }
      "goodkind-mwan-steering:accept-ra"   = false
    }
  }

  provider_sonic_1 = {
    name                                   = local.sonic_1_interface
    type                                   = "iana-if-type:other"
    "goodkind-mwan-steering:connection-id" = "sonic_1"
    "goodkind-mwan-steering:owner"         = "mwan"
    "goodkind-mwan-steering:link" = {
      match = { "hardware-address" = lower(local.gateway_map.routing_interfaces.sonic_1.mac_address) }
    }
    "goodkind-mwan-steering:wan" = {
      name           = "sonic_1"
      "table-id"     = 800
      "fw-mark"      = 6
      "fw-mark-prio" = 600
      "from-prio"    = 60
      "forced-dscp"  = 35
      health = merge(local.link_health, {
        "targets-v4" = [local.sonic_1_link_ipv4]
        "targets-v6" = []
        "http-urls"  = []
      })
    }
    "goodkind-mwan-steering:steering" = { tier = 3, weight = 1 }
    "ietf-ip:ipv4" = {
      "goodkind-mwan-steering:translation"  = { mode = "ietf-nat:napt44" }
      forwarding                            = true
      "goodkind-mwan-steering:dhcp"         = false
      "goodkind-mwan-steering:gateway"      = local.sonic_1_link_ipv4
      "goodkind-mwan-steering:route-metric" = 8000
      address                               = [{ ip = local.mwan_sonic_1_ipv4, "prefix-length" = tonumber(split("/", local.routing_networks.sonic_1.ipv4_net)[1]) }]
    }
    "ietf-ip:ipv6" = {
      "goodkind-mwan-steering:translation" = { mode = "native" }
      "goodkind-mwan-steering:accept-ra"   = false
    }
  }

  provider_sonic_2 = {
    name                                   = local.sonic_2_interface
    type                                   = "iana-if-type:other"
    "goodkind-mwan-steering:connection-id" = "sonic_2"
    "goodkind-mwan-steering:owner"         = "mwan"
    "goodkind-mwan-steering:link" = {
      match = { "hardware-address" = lower(local.gateway_map.routing_interfaces.sonic_2.mac_address) }
    }
    "goodkind-mwan-steering:wan" = {
      name           = "sonic_2"
      "table-id"     = 900
      "fw-mark"      = 7
      "fw-mark-prio" = 700
      "from-prio"    = 61
      "forced-dscp"  = 39
      health = merge(local.link_health, {
        "targets-v4" = [local.sonic_2_link_ipv4]
        "targets-v6" = []
        "http-urls"  = []
      })
    }
    "goodkind-mwan-steering:steering" = { tier = 3, weight = 1 }
    "ietf-ip:ipv4" = {
      "goodkind-mwan-steering:translation"  = { mode = "ietf-nat:napt44" }
      forwarding                            = true
      "goodkind-mwan-steering:dhcp"         = false
      "goodkind-mwan-steering:gateway"      = local.sonic_2_link_ipv4
      "goodkind-mwan-steering:route-metric" = 8100
      address                               = [{ ip = local.mwan_sonic_2_ipv4, "prefix-length" = tonumber(split("/", local.routing_networks.sonic_2.ipv4_net)[1]) }]
    }
    "ietf-ip:ipv6" = {
      "goodkind-mwan-steering:translation" = { mode = "native" }
      "goodkind-mwan-steering:accept-ra"   = false
    }
  }
}

# The BGP sessions and tunnels of the providers, ported from
# ansible/inventory/group_vars/testbed_routing_all/scenario_*.yml.
locals {
  etheric_remote_net  = local.routing_networks.etheric_native_remote.ipv6_net
  upstream_remote_net = local.routing_networks.tunnel_upstream_remote.ipv6_net
  vps_remote_net      = local.routing_networks.tunnel_vps_remote.ipv6_net
  static_remote_net   = local.routing_networks.tunnel_static_remote.ipv6_net

  # A prefix list entry restricts the length to the prefix length.
  etheric_import = [
    {
      prefix       = local.etheric_remote_net
      "min-length" = tonumber(split("/", local.etheric_remote_net)[1])
      "max-length" = tonumber(split("/", local.etheric_remote_net)[1])
    },
    local.import_prefix_all,
  ]
  upstream_import = [
    {
      prefix       = local.upstream_remote_net
      "min-length" = tonumber(split("/", local.upstream_remote_net)[1])
      "max-length" = tonumber(split("/", local.upstream_remote_net)[1])
    },
    local.import_prefix_all,
  ]
  vps_import = [
    {
      prefix       = local.vps_remote_net
      "min-length" = tonumber(split("/", local.vps_remote_net)[1])
      "max-length" = tonumber(split("/", local.vps_remote_net)[1])
    },
    local.import_prefix_all,
  ]

  bgp_connect_retry_seconds    = 5
  bgp_graceful_restart_seconds = 60

  provider_etheric = {
    name                                   = local.etheric_interface
    type                                   = "iana-if-type:other"
    "goodkind-mwan-steering:connection-id" = "etheric"
    "goodkind-mwan-steering:owner"         = "mwan"
    "goodkind-mwan-steering:link" = {
      match = { "hardware-address" = lower(local.gateway_map.routing_interfaces.etheric.mac_address) }
    }
    "goodkind-mwan-steering:wan" = {
      name           = "etheric"
      "table-id"     = 1000
      "fw-mark"      = 8
      "fw-mark-prio" = 800
      "from-prio"    = 62
      "forced-dscp"  = 43
      health = merge(local.link_health, {
        "targets-v4" = [local.etheric_link_ipv4]
        "targets-v6" = [local.etheric_link_ipv6]
        "http-urls"  = []
      })
      "bgp-session" = [{
        name                    = "etheric_native_home"
        "peer-address"          = local.etheric_link_ipv6
        "local-address"         = local.mwan_etheric_ipv6
        "local-as"              = 64512
        "remote-as"             = 64520
        family                  = ["ipv6-unicast"]
        keepalive               = 3
        hold                    = 9
        "connect-retry"         = local.bgp_connect_retry_seconds
        "route-metric"          = 8500
        "graceful-restart-time" = local.bgp_graceful_restart_seconds
        import                  = local.etheric_import
        export                  = [{ prefix = local.home_prefix, mode = "always", "next-hop" = local.mwan_etheric_ipv6 }]
      }]
    }
    "goodkind-mwan-steering:steering" = { tier = 3, weight = 1 }
    "ietf-ip:ipv4" = {
      "goodkind-mwan-steering:translation"  = { mode = "ietf-nat:napt44" }
      forwarding                            = true
      "goodkind-mwan-steering:dhcp"         = false
      "goodkind-mwan-steering:gateway"      = local.etheric_link_ipv4
      "goodkind-mwan-steering:route-metric" = 8200
      address                               = [{ ip = local.mwan_etheric_ipv4, "prefix-length" = tonumber(split("/", local.routing_networks.etheric.ipv4_net)[1]) }]
    }
    "ietf-ip:ipv6" = {
      "goodkind-mwan-steering:translation" = { mode = "native" }
      forwarding                           = true
      "goodkind-mwan-steering:accept-ra"   = false
      address                              = [{ ip = local.mwan_etheric_ipv6, "prefix-length" = tonumber(split("/", local.routing_networks.etheric.ipv6_net)[1]) }]
    }
  }

  tunnel_astound_static = {
    peer_inner   = "2001:db8:1ff:1::2"
    local_inner  = "2001:db8:1ff:1::1"
    local_outer  = local.mwan_astound_ipv4
    remote_outer = local.astound_sim.routing_interfaces.outer.ipv4
  }
  tunnel_static_sonic_1 = {
    peer_inner   = "2001:db8:3ff:1::2"
    local_inner  = "2001:db8:3ff:1::1"
    local_outer  = local.mwan_sonic_1_ipv4
    remote_outer = local.static_sim.routing_interfaces.outer.ipv4
  }
  tunnel_upstream_sonic_2 = {
    peer_inner   = "2001:db8:4ff:1::2"
    local_inner  = "2001:db8:4ff:1::1"
    local_outer  = local.mwan_sonic_2_ipv4
    remote_outer = local.upstream_sim.routing_interfaces.outer.ipv4
  }
  tunnel_vps_sonic_1 = {
    peer_inner   = "2001:db8:5ff:1::2"
    local_inner  = "2001:db8:5ff:1::1"
    local_outer  = local.mwan_sonic_1_ipv4
    remote_outer = local.vps_sim.routing_interfaces.outer.ipv4
  }
  tunnel_vps_astound = {
    peer_inner   = "2001:db8:5ff:2::2"
    local_inner  = "2001:db8:5ff:2::1"
    local_outer  = local.mwan_astound_ipv4
    remote_outer = local.vps_sim.routing_interfaces.outer.ipv4
  }

  tunnel_mtu         = 1480
  tunnel_ttl         = 64
  tunnel_prefix_size = 64

  provider_tunnel_astound_static = {
    name                                   = "sit-astound"
    type                                   = "iana-if-type:tunnel"
    "goodkind-mwan-steering:connection-id" = "astound_static_astound"
    "goodkind-mwan-steering:owner"         = "mwan"
    "goodkind-mwan-steering:link" = {
      mtu = local.tunnel_mtu
      tunnel = {
        protocol         = "6in4"
        underlay         = local.astound_interface
        "remote-address" = local.tunnel_astound_static.remote_outer
        "local-address"  = local.tunnel_astound_static.local_outer
        ttl              = local.tunnel_ttl
      }
    }
    "goodkind-mwan-steering:wan" = {
      name           = "astound_static_astound"
      "table-id"     = 1100
      "fw-mark"      = 9
      "fw-mark-prio" = 900
      "from-prio"    = 63
      "forced-dscp"  = 47
      health = merge(local.link_health, {
        "targets-v4" = []
        "targets-v6" = [local.tunnel_astound_static.peer_inner]
        "http-urls"  = []
      })
    }
    "goodkind-mwan-steering:steering" = { tier = 3, weight = 1 }
    "ietf-ip:ipv6" = {
      "goodkind-mwan-steering:translation"  = { mode = "native" }
      address                               = [{ ip = local.tunnel_astound_static.local_inner, "prefix-length" = local.tunnel_prefix_size }]
      "goodkind-mwan-steering:gateway"      = local.tunnel_astound_static.peer_inner
      "goodkind-mwan-steering:route-metric" = 8300
      "goodkind-mwan-steering:route" = [{
        destination = local.routing_networks.astound_static_remote.ipv6_net
        gateway     = local.tunnel_astound_static.peer_inner
      }]
    }
  }

  provider_tunnel_static_sonic_1 = {
    name                                   = "sit-static"
    type                                   = "iana-if-type:tunnel"
    "goodkind-mwan-steering:connection-id" = "tunnel_static_sonic_1"
    "goodkind-mwan-steering:owner"         = "mwan"
    "goodkind-mwan-steering:link" = {
      mtu = local.tunnel_mtu
      tunnel = {
        protocol         = "6in4"
        underlay         = local.sonic_1_interface
        "remote-address" = local.tunnel_static_sonic_1.remote_outer
        "local-address"  = local.tunnel_static_sonic_1.local_outer
        ttl              = local.tunnel_ttl
      }
    }
    "goodkind-mwan-steering:wan" = {
      name           = "tunnel_static_sonic_1"
      "table-id"     = 1200
      "fw-mark"      = 10
      "fw-mark-prio" = 1000
      "from-prio"    = 64
      "forced-dscp"  = 51
      health = merge(local.link_health, {
        "targets-v4" = []
        "targets-v6" = [local.tunnel_static_sonic_1.peer_inner]
        "http-urls"  = []
      })
    }
    "goodkind-mwan-steering:steering" = { tier = 3, weight = 1 }
    "ietf-ip:ipv6" = {
      "goodkind-mwan-steering:translation"  = { mode = "native" }
      address                               = [{ ip = local.tunnel_static_sonic_1.local_inner, "prefix-length" = local.tunnel_prefix_size }]
      "goodkind-mwan-steering:gateway"      = local.tunnel_static_sonic_1.peer_inner
      "goodkind-mwan-steering:route-metric" = 8400
      "goodkind-mwan-steering:route" = [{
        destination = local.static_remote_net
        gateway     = local.tunnel_static_sonic_1.peer_inner
      }]
    }
  }

  provider_tunnel_upstream_sonic_2 = {
    name                                   = "sit-upstream"
    type                                   = "iana-if-type:tunnel"
    "goodkind-mwan-steering:connection-id" = "tunnel_upstream_sonic_2"
    "goodkind-mwan-steering:owner"         = "mwan"
    "goodkind-mwan-steering:link" = {
      mtu = local.tunnel_mtu
      tunnel = {
        protocol         = "6in4"
        underlay         = local.sonic_2_interface
        "remote-address" = local.tunnel_upstream_sonic_2.remote_outer
        "local-address"  = local.tunnel_upstream_sonic_2.local_outer
        ttl              = local.tunnel_ttl
      }
    }
    "goodkind-mwan-steering:wan" = {
      name           = "tunnel_upstream_sonic_2"
      "table-id"     = 1300
      "fw-mark"      = 11
      "fw-mark-prio" = 1100
      "from-prio"    = 65
      "forced-dscp"  = 55
      health = merge(local.link_health, {
        "targets-v4" = []
        "targets-v6" = [local.tunnel_upstream_sonic_2.peer_inner]
        "http-urls"  = []
      })
      "bgp-session" = [{
        name                    = "tunnel_upstream_home"
        "peer-address"          = local.service_mapping.routing_tunnel_upstream_upstream_suburban.routing_interfaces.transit.ipv6
        "local-address"         = local.tunnel_upstream_sonic_2.local_inner
        "local-as"              = 64512
        "remote-as"             = 64541
        family                  = ["ipv6-unicast"]
        keepalive               = 3
        hold                    = 9
        "connect-retry"         = local.bgp_connect_retry_seconds
        "route-metric"          = 8600
        "multihop-ttl"          = 2
        "graceful-restart-time" = local.bgp_graceful_restart_seconds
        import                  = local.upstream_import
        export                  = [{ prefix = local.home_prefix, mode = "always", "next-hop" = local.tunnel_upstream_sonic_2.local_inner }]
      }]
    }
    "goodkind-mwan-steering:steering" = { tier = 3, weight = 1 }
    "ietf-ip:ipv6" = {
      "goodkind-mwan-steering:translation" = { mode = "native" }
      address                              = [{ ip = local.tunnel_upstream_sonic_2.local_inner, "prefix-length" = local.tunnel_prefix_size }]
      "goodkind-mwan-steering:route" = [{
        destination = local.routing_networks.tunnel_upstream_transit.ipv6_net
        gateway     = local.tunnel_upstream_sonic_2.peer_inner
      }]
    }
  }

  provider_tunnel_vps_sonic_1 = {
    name                                   = "sit-vps-sonic"
    type                                   = "iana-if-type:tunnel"
    "goodkind-mwan-steering:connection-id" = "tunnel_vps_sonic_1"
    "goodkind-mwan-steering:owner"         = "mwan"
    "goodkind-mwan-steering:link" = {
      mtu = local.tunnel_mtu
      tunnel = {
        protocol         = "6in4"
        underlay         = local.sonic_1_interface
        "remote-address" = local.tunnel_vps_sonic_1.remote_outer
        "local-address"  = local.tunnel_vps_sonic_1.local_outer
        ttl              = local.tunnel_ttl
      }
    }
    "goodkind-mwan-steering:wan" = {
      name           = "tunnel_vps_sonic_1"
      "table-id"     = 1400
      "fw-mark"      = 12
      "fw-mark-prio" = 1200
      "from-prio"    = 66
      "forced-dscp"  = 59
      health = merge(local.link_health, {
        "targets-v4" = []
        "targets-v6" = [local.tunnel_vps_sonic_1.peer_inner]
        "http-urls"  = []
      })
      "bgp-session" = [{
        name                    = "tunnel_vps_home_sonic_1"
        "peer-address"          = local.tunnel_vps_sonic_1.peer_inner
        "local-address"         = local.tunnel_vps_sonic_1.local_inner
        "local-as"              = 64512
        "remote-as"             = 64550
        family                  = ["ipv6-unicast"]
        keepalive               = 3
        hold                    = 9
        "connect-retry"         = local.bgp_connect_retry_seconds
        "route-metric"          = 8700
        "graceful-restart-time" = local.bgp_graceful_restart_seconds
        import                  = local.vps_import
        export                  = [{ prefix = local.home_prefix, mode = "always", "next-hop" = local.tunnel_vps_sonic_1.local_inner }]
      }]
    }
    "goodkind-mwan-steering:steering" = { tier = 3, weight = 1 }
    "ietf-ip:ipv6" = {
      "goodkind-mwan-steering:translation" = { mode = "native" }
      address                              = [{ ip = local.tunnel_vps_sonic_1.local_inner, "prefix-length" = local.tunnel_prefix_size }]
    }
  }

  provider_tunnel_vps_astound = {
    name                                   = "sit-vps-astound"
    type                                   = "iana-if-type:tunnel"
    "goodkind-mwan-steering:connection-id" = "tunnel_vps_astound"
    "goodkind-mwan-steering:owner"         = "mwan"
    "goodkind-mwan-steering:link" = {
      mtu = local.tunnel_mtu
      tunnel = {
        protocol         = "6in4"
        underlay         = local.astound_interface
        "remote-address" = local.tunnel_vps_astound.remote_outer
        "local-address"  = local.tunnel_vps_astound.local_outer
        ttl              = local.tunnel_ttl
      }
    }
    "goodkind-mwan-steering:wan" = {
      name           = "tunnel_vps_astound"
      "table-id"     = 1500
      "fw-mark"      = 13
      "fw-mark-prio" = 1300
      "from-prio"    = 67
      "forced-dscp"  = 63
      health = merge(local.link_health, {
        "targets-v4" = []
        "targets-v6" = [local.tunnel_vps_astound.peer_inner]
        "http-urls"  = []
      })
      "bgp-session" = [{
        name                    = "tunnel_vps_home_astound"
        "peer-address"          = local.tunnel_vps_astound.peer_inner
        "local-address"         = local.tunnel_vps_astound.local_inner
        "local-as"              = 64512
        "remote-as"             = 64550
        family                  = ["ipv6-unicast"]
        keepalive               = 3
        hold                    = 9
        "connect-retry"         = local.bgp_connect_retry_seconds
        "route-metric"          = 8800
        "graceful-restart-time" = local.bgp_graceful_restart_seconds
        import                  = local.vps_import
        export                  = [{ prefix = local.home_prefix, mode = "always", "next-hop" = local.tunnel_vps_astound.local_inner }]
      }]
    }
    "goodkind-mwan-steering:steering" = { tier = 3, weight = 1 }
    "ietf-ip:ipv6" = {
      "goodkind-mwan-steering:translation" = { mode = "native" }
      address                              = [{ ip = local.tunnel_vps_astound.local_inner, "prefix-length" = local.tunnel_prefix_size }]
    }
  }
}

# Links the daemon owns that are not providers.
locals {
  internal_connection = {
    name                                   = local.internal_interface
    type                                   = "iana-if-type:other"
    "goodkind-mwan-steering:connection-id" = "enmwanbr0"
    "goodkind-mwan-steering:owner"         = "mwan"
    "goodkind-mwan-steering:link" = {
      match = { "hardware-address" = local.gateway_macs.internal }
    }
    "ietf-ip:ipv4" = {
      address    = [{ ip = local.gateway_map.ipv4_transit, "prefix-length" = tonumber(split("/", local.router_map.transit_v4_net)[1]) }]
      forwarding = true
    }
    "ietf-ip:ipv6" = {
      address                            = [{ ip = local.gateway_map.ipv6_transit, "prefix-length" = tonumber(split("/", local.router_map.transit_v6_net)[1]) }]
      forwarding                         = true
      "goodkind-mwan-steering:accept-ra" = false
      "goodkind-mwan-steering:route" = [{
        destination = local.home_prefix
        gateway     = local.router_map.ipv6_transit
        "table-id"  = 254
        metric      = 0
      }]
    }
  }

  management_connection = {
    name                                   = local.management_iface
    type                                   = "iana-if-type:other"
    "goodkind-mwan-steering:connection-id" = "enmgmt0"
    "goodkind-mwan-steering:owner"         = "mwan"
    "goodkind-mwan-steering:link" = {
      match = { "hardware-address" = local.gateway_macs.management }
    }
    "ietf-ip:ipv4" = {
      "goodkind-mwan-steering:dhcp"     = false
      "goodkind-mwan-steering:resolver" = { dns = [], search = [] }
    }
    "ietf-ip:ipv6" = {
      address                            = [{ ip = local.gateway_map.ipv6, "prefix-length" = 64 }]
      "goodkind-mwan-steering:accept-ra" = false
      "goodkind-mwan-steering:resolver" = {
        dns    = [local.router_map.ipv6_vmnet]
        search = ["home.goodkind.io"]
      }
    }
  }

  network_document = {
    "ietf-interfaces:interfaces" = {
      interface = [
        local.provider_att,
        local.provider_webpass,
        local.provider_tertiary,
        local.provider_routed,
        local.provider_astound,
        local.provider_sonic_1,
        local.provider_sonic_2,
        local.provider_etheric,
        local.provider_tunnel_astound_static,
        local.provider_tunnel_static_sonic_1,
        local.provider_tunnel_upstream_sonic_2,
        local.provider_tunnel_vps_sonic_1,
        local.provider_tunnel_vps_astound,
        local.internal_connection,
        local.management_connection,
      ]
      "goodkind-mwan-steering:steering-group" = {
        "hash-mode"       = "random"
        "reserved-tables" = local.reserved_tables
        translation = {
          "internal-prefix"  = local.internal_prefix
          "opnsense-edge-v6" = local.router_map.ipv6_transit
          "mwanbr-edge-v6"   = local.gateway_map.ipv6_transit
        }
        routes = {
          "internal-iface"  = local.internal_interface
          "internal-net-v4" = local.router_map.transit_v4_net
        }
        firewall = {
          "management-interface" = local.management_iface
          "management-service" = [
            { protocol = "tcp", port = 22 },
            { protocol = "tcp", port = 50052 },
            { protocol = "tcp", port = local.restconf_port },
          ]
          "pinned-provider"         = "att"
          "pinned-source-v4"        = local.router_map.ipv4_transit
          "pinned-source-port"      = 51820
          "pinned-destination-port" = 51821
          "pinned-set-v4-name"      = "att_pinned_v4"
          "pinned-set-v6-name"      = "att_pinned_v6"
          "pinned-v4"               = ["1.0.0.1/32"]
          "pinned-v6"               = ["2606:4700:4700::1001/128"]
        }
        health = { "probe-timeout" = 2000 }
      }
    }
  }

  gateway_network_json = jsonencode(local.network_document)
}

# Runtime configuration text of the gateway and the failover. The gateway text
# contains secrets. The module writes it through the write-only attribute.
locals {
  gateway_config_values = {
    hostname                = "suburban-testbed"
    vmid                    = local.gateway_map.vmid
    management_address      = local.gateway_map.ipv6
    email_recipient         = local.alert_recipient
    email_from              = "mwan-testbed@goodkind.io"
    email_subject_prefix    = "[MWAN-suburban]"
    email_bind_iface        = ""
    email_min_level         = local.alert_level
    email_cooldown          = local.alert_cooldown
    pve_base_url            = "https://[::1]:8006/api2/json"
    pve_node                = "hypervisor"
    pve_token_id            = local.pve_token_id
    watchdog_service_name   = "mwan-watchdog-testbed"
    watchdog_agent_tcp_addr = "[${local.gateway_map.ipv6}]:50052"
    watchdog_log_file       = "/var/log/mwan-watchdog-testbed.log"
    watchdog_json_log_file  = "/var/log/mwan-watchdog-testbed.jsonl"
    watchdog_state_file     = "/run/mwan-rollback-testbed.state"
    watchdog_lock_file      = "/run/mwan-watchdog-testbed-rollback.lock"
    failover_vmid           = local.failover_map.vmid
    opnsense_url            = "https://[${local.router_map.ipv6_vmnet}]"
    opnsense_gateway_names  = ["WAN_GW4", "WAN_GW6"]
    nptv6_hairpin_alias     = "mwan_npt_hairpin_sources"
    nptv6_hairpin_ca_file   = "/etc/mwan/opnsense-testbed.pem"
    opnsense_bgp_router_id  = local.router_map.ipv4_transit

    opnsense_bgp_firewall_source_v4 = local.router_map.transit_v4_net
    opnsense_bgp_firewall_source_v6 = local.router_map.transit_v6_net
    opnsense_bgp_neighbors = [
      { address = local.gateway_map.ipv4_transit, description = "mwan-IPv4", preference = "primary" },
      { address = local.gateway_map.ipv6_transit, description = "mwan-IPv6", preference = "primary" },
      { address = local.failover_map.ipv4_transit, description = "failover-IPv4", preference = "backup" },
      { address = local.failover_map.ipv6_transit, description = "failover-IPv6", preference = "backup" },
    ]

    bgp_asn                 = local.bgp_asn
    bgp_router_id           = local.gateway_map.ipv4_transit
    bgp_next_hop_v6         = local.gateway_map.ipv6_transit
    bgp_keepalive_seconds   = local.bgp_keepalive_seconds
    bgp_hold_seconds        = local.bgp_hold_seconds
    bgp_listen_port         = local.bgp_listen_port
    bgp_learned_route_iface = local.internal_interface
    bgp_dynamic_neighbors   = local.transit_networks
    bgp_neighbors           = [local.router_map.ipv4_transit]
    bgp_neighbors_v6        = [local.router_map.ipv6_transit]
    bgp_announce_ipv4       = local.bgp_announce_v4
    bgp_announce_ipv6       = local.bgp_announce_v6

    bgp_graceful_restart_enabled                        = false
    bgp_graceful_restart_time_seconds                   = 30
    bgp_graceful_restart_notification_enabled           = true
    bgp_forwarding_readiness_socket_path                = "/run/mwan-forwarding-ready.sock"
    bgp_forwarding_readiness_poll_interval_milliseconds = 1000
    bgp_forwarding_readiness_read_timeout_milliseconds  = 500

    ifmgr_iface_key   = local.mbrains_interface
    ifmgr_iface_name  = local.mbrains_interface
    links_state_file  = "/var/lib/mwan/owned-links.json"
    health_state_file = "/var/run/mwan-health.state"
    status_push_port  = local.status_push_port

    autoconfiguration_state_file = "/var/lib/mwan/owned-kernel.json"
    bgp_sessions_state_file      = "/var/lib/mwan/owned-bgp-sessions.json"
    resolver_state_file          = local.resolver_state_file
  }

  gateway_config_public = templatefile("${path.module}/gateway-config.toml.tftpl", {
    c = merge(local.gateway_config_values, {
      smtp2go_api_key     = ""
      pve_token_secret    = ""
      opnsense_api_key    = ""
      opnsense_api_secret = ""
    })
  })

  gateway_config_secret = templatefile("${path.module}/gateway-config.toml.tftpl", {
    c = merge(local.gateway_config_values, {
      smtp2go_api_key     = var.vault_smtp2go_api_key
      pve_token_secret    = var.vault_suburban_testbed_pve_token_secret
      opnsense_api_key    = var.vault_suburban_testbed_opnsense_api_key
      opnsense_api_secret = var.vault_suburban_testbed_opnsense_api_secret
    })
  })

  # The version derives from the text without secrets and from an operator
  # serial for the secret values. A rotated secret needs a new serial.
  gateway_config_version = parseint(substr(sha256(local.gateway_config_public), 0, 8), 16) + var.gateway_secret_serial

  failover_config = templatefile("${path.module}/failover-config.toml.tftpl", {
    c = {
      hostname                                  = split(".", local.failover_map.hostname)[0]
      email_recipient                           = local.alert_recipient
      email_from                                = "mwan-testbed@goodkind.io"
      email_subject_prefix                      = "[MWAN-suburban-failover]"
      email_bind_iface                          = ""
      email_min_level                           = local.alert_level
      email_cooldown                            = local.alert_cooldown
      bgp_asn                                   = local.bgp_asn
      bgp_router_id                             = local.failover_map.ipv4_transit
      bgp_next_hop_v6                           = local.failover_map.ipv6_transit
      bgp_keepalive_seconds                     = local.bgp_keepalive_seconds
      bgp_hold_seconds                          = local.bgp_hold_seconds
      bgp_listen_port                           = local.bgp_listen_port
      bgp_learned_route_iface                   = "eth1"
      bgp_dynamic_neighbors                     = local.transit_networks
      bgp_neighbors                             = [local.router_map.ipv4_transit]
      bgp_neighbors_v6                          = [local.router_map.ipv6_transit]
      bgp_announce_ipv4                         = local.bgp_announce_v4
      bgp_announce_ipv6                         = local.bgp_announce_v6
      bgp_graceful_restart_enabled              = true
      bgp_graceful_restart_time_seconds         = 30
      bgp_graceful_restart_notification_enabled = true
    }
  })
}

module "mwan" {
  source = "../../modules/mwan"

  node = local.guests["mwan_suburban"].node
  vmid = local.guests["mwan_suburban"].vmid
  kind = local.guests["mwan_suburban"].kind

  role            = local.gateway_role
  architecture    = local.mwan_arch
  release_version = local.mwan_release
  excluded_units  = local.excluded_units
  sysctl_values   = local.gateway_sysctl

  network_json_content        = local.gateway_network_json
  config_toml_secret_content  = local.gateway_config_secret
  config_toml_content_version = local.gateway_config_version
}

module "mwan_failover" {
  source = "../../modules/mwan"

  node = local.guests["mwan_failover_suburban"].node
  vmid = local.guests["mwan_failover_suburban"].vmid
  kind = local.guests["mwan_failover_suburban"].kind

  role            = local.failover_role
  architecture    = local.mwan_arch
  release_version = local.mwan_release
  excluded_units  = local.excluded_units
  sysctl_values   = local.failover_sysctl

  config_toml_content = local.failover_config
}

# Ansible wrote these files on the hosts before this root managed them. The
# provider imports the file hash without rewriting the file. The import ID
# is node/vmid/kind/absolute-path, and the absolute path starts with a slash.
import {
  to = module.mwan.pveguest_file.runtime_config
  id = "${local.guests["mwan_suburban"].node}/${local.guests["mwan_suburban"].vmid}/${local.guests["mwan_suburban"].kind}//etc/mwan/config.toml"
}

import {
  to = module.mwan.pveguest_file.network[0]
  id = "${local.guests["mwan_suburban"].node}/${local.guests["mwan_suburban"].vmid}/${local.guests["mwan_suburban"].kind}//etc/mwan/network.json"
}

import {
  to = module.mwan_failover.pveguest_file.runtime_config
  id = "${local.guests["mwan_failover_suburban"].node}/${local.guests["mwan_failover_suburban"].vmid}/${local.guests["mwan_failover_suburban"].kind}//etc/mwan/config.toml"
}

output "mwan_release" {
  description = "Release tag that both MWAN instances install."
  value       = local.mwan_release
}

output "mwan_gateway_config_version" {
  description = "Write-only version of the gateway runtime configuration."
  value       = local.gateway_config_version
}
