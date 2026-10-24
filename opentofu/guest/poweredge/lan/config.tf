locals {
  template_vars = { nat64_prefix = var.nat64_prefix }
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
  services = {
    "kea-dhcp4-server.service" = ["dhcp4"]
    "kea-dhcp6-server.service" = ["dhcp6"]
    "named.service"            = ["dns", "dns_network"]
    "nftables.service"         = ["firewall"]
    "vpp.service"              = ["vpp_startup", "vpp_nat64", "vpp_service"]
    "nginx.service"            = ["nginx", "beacon", "beacon_certificate", "nginx_network"]
  }
}
