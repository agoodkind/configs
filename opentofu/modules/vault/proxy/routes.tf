locals {
  primary_certificate = "router.home.goodkind.io"

  http_routes = {
    (local.primary_certificate)    = { origin = "https://[3d06:bad:b01::1]" }
    "controller.home.goodkind.io"  = { origin = "https://unifi.home.goodkind.io:8443", upstream_sni = "UniFi" }
    "vault.home.goodkind.io"       = { origin = "https://[${local.service_mapping.vault_hypervisor.ipv6}]:8006" }
    "datastore.home.goodkind.io"   = { origin = "http://nas.home.goodkind.io:3923" }
    "adguard.home.goodkind.io"     = { origin = "https://[3d06:bad:b01::53]", internal_only = true }
    "mdm.home.goodkind.io"         = { origin = "http://[3d06:bad:b01::103]:9000" }
    "assistant.home.goodkind.io"   = { origin = "http://[3d06:bad:b01:2::3]:8123", hsts = false }
    "freebsd-dev.home.goodkind.io" = { origin = "http://[3d06:bad:b01::108]:8080" }
    "mc.home.goodkind.io"          = { origin = "https://[3d06:bad:b01::109]:8443" }
  }

  stream_routes = [
    { port = 25565, protocol = "tcp", origin = "[3d06:bad:b01::109]:25565" },
    { port = 22000, protocol = "tcp", origin = "[3d06:bad:b01:1::3]:22000" },
    { port = 9876, protocol = "udp", origin = "[3d06:bad:b01::100]:9876" },
    { port = 22000, protocol = "udp", origin = "[3d06:bad:b01:1::3]:22000" },
    { port = 22001, protocol = "udp", origin = "[3d06:bad:b01:1::3]:22001" },
  ]
}
