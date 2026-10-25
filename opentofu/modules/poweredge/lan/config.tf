locals {
  template_vars = {
    nat64_prefix         = var.nat64_prefix
    collingwood_networks = local.collingwood_networks
    collingwood_zones    = local.collingwood_zones
  }
  # FRR installs before the container can learn its default routes through BGP.
  frr_debs = jsondecode(file("${path.module}/../../lanrouter/frr-packages.json"))
  network_files = {
    "20-transit.network"   = "20-transit.network"
    "30-lan.network"       = "30-lan.network"
    "40-lan-out.network"   = "40-lan-out.network"
    "50-nat64-in.network"  = "50-nat64-in.network.tftpl"
    "51-nat64-out.network" = "51-nat64-out.network"
  }
  file_defaults = {
    owner    = "root"
    group    = "root"
    mode     = "0644"
    validate = null
  }
  service_file_overrides = {
    frr = {
      path     = "/etc/frr/frr.conf"
      source   = "frr.conf"
      owner    = "frr"
      group    = "frr"
      mode     = "0640"
      validate = "/usr/bin/vtysh --dryrun --inputfile %s"
    }
    dhcp4 = {
      path     = "/etc/kea/kea-dhcp4.conf"
      source   = "kea-dhcp4.conf"
      group    = "_kea"
      mode     = "0640"
      validate = "/usr/sbin/kea-dhcp4 -t %s"
    }
    dhcp6 = {
      path     = "/etc/kea/kea-dhcp6.conf"
      source   = "kea-dhcp6.conf"
      group    = "_kea"
      mode     = "0640"
      validate = "/usr/sbin/kea-dhcp6 -t %s"
    }
    ddns = {
      path     = "/etc/kea/kea-dhcp-ddns.conf"
      source   = "kea-dhcp-ddns.conf"
      group    = "_kea"
      mode     = "0640"
      validate = "/usr/sbin/kea-dhcp-ddns -t %s"
    }
    dhcp4_ddns = {
      path   = "/etc/systemd/system/kea-dhcp4-server.service.d/ddns.conf"
      source = "dhcp-ddns-required.conf"
    }
    dhcp6_ddns = {
      path   = "/etc/systemd/system/kea-dhcp6-server.service.d/ddns.conf"
      source = "dhcp-ddns-required.conf"
    }
    ddns_dns = {
      path   = "/etc/systemd/system/kea-dhcp-ddns-server.service.d/dns.conf"
      source = "ddns-dns-required.conf"
    }
    dns = {
      path     = "/etc/bind/named.conf.options"
      source   = "named.conf.options.tftpl"
      group    = "bind"
      validate = "/usr/bin/named-checkconf %s"
    }
    dns_network = {
      path   = "/etc/systemd/system/named.service.d/network.conf"
      source = "named-network.conf"
    }
    dns_zones = {
      path     = "/etc/bind/named.conf.local"
      source   = "named-ddns.conf"
      group    = "bind"
      validate = "/usr/bin/named-checkconf %s"
    }
    firewall = {
      path     = "/etc/nftables.conf"
      source   = "nftables.conf"
      validate = "/usr/sbin/nft --check --file %s"
    }
    vpp_startup = {
      path   = "/etc/vpp/startup.conf"
      source = "startup.conf"
    }
    vpp_nat64 = {
      path   = "/etc/vpp/nat64.cli"
      source = "nat64.cli.tftpl"
    }
    vpp_service = {
      path   = "/etc/systemd/system/vpp.service.d/nat64.conf"
      source = "vpp-nat64.conf"
    }
    nginx = {
      path   = "/etc/nginx/nginx.conf"
      source = "nginx.conf"
    }
    beacon = {
      path   = "/etc/nginx/conf.d/cloudflare-beacon.conf"
      source = "cloudflare-beacon.conf"
    }
    beacon_certificate = {
      path     = "/etc/cloudflare-beacon/cert.pem"
      source   = "beacon-cert.pem"
      validate = "/usr/bin/openssl x509 -in %s -noout"
    }
    nginx_network = {
      path   = "/etc/systemd/system/nginx.service.d/network.conf"
      source = "nginx-network.conf"
    }
  }
  service_files = {
    for name, config in local.service_file_overrides : name => merge(local.file_defaults, config)
  }
  service_content = merge(
    { for name, config in local.service_files : name => templatefile("${path.module}/files/${config.source}", local.template_vars) },
    { for name, config in local.collingwood_kea_configs : name => jsonencode(config) }
  )
  services = {
    "kea-dhcp4-server.service"     = ["dhcp4", "dhcp4_ddns"]
    "kea-dhcp6-server.service"     = ["dhcp6", "dhcp6_ddns"]
    "kea-dhcp-ddns-server.service" = ["ddns", "ddns_dns"]
    "named.service"                = ["dns", "dns_network", "dns_zones"]
    "vpp.service"                  = ["vpp_startup", "vpp_nat64", "vpp_service"]
    "nginx.service"                = ["nginx", "beacon", "beacon_certificate", "nginx_network"]
  }
}
