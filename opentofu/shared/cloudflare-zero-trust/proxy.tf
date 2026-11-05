resource "cloudflare_zero_trust_tunnel_cloudflared_config" "proxy" {
  provider = cloudflare.tunnels

  account_id = var.cloudflare_account_id
  tunnel_id  = module.contracts.tack_tunnel_id
  source     = "cloudflare"
  config = {
    ingress = concat([
      {
        hostname = "mdm.goodkind.io"
        service  = "https://mdm.home.goodkind.io"
        origin_request = {
          http2_origin     = true
          http_host_header = "mdm.home.goodkind.io"
        }
      },
      {
        hostname = "home-assistant-ext.goodkind.io"
        service  = "https://assistant.home.goodkind.io"
        origin_request = {
          http2_origin       = true
          http_host_header   = "assistant.home.goodkind.io"
          origin_server_name = ""
        }
      },
      {
        hostname = "cloudflared-opnsense-pkg.goodkind.io"
        service  = "https://localhost"
        origin_request = {
          http2_origin       = true
          http_host_header   = "freebsd-dev.home.goodkind.io"
          origin_server_name = "freebsd-dev.home.goodkind.io"
        }
      },
      {
        hostname = "plane.goodkind.io"
        service  = "https://plane.home.goodkind.io"
        origin_request = {
          http2_origin       = true
          no_tls_verify      = true
          http_host_header   = "plane.home.goodkind.io"
          match_sn_ito_host  = true
          origin_server_name = "plane.home.goodkind.io"
        }
      },
      {
        hostname = "holy.goodkind.io"
        service  = "https://huly.home.goodkind.io"
        origin_request = {
          http2_origin       = true
          no_tls_verify      = true
          http_host_header   = "huly.home.goodkind.io"
          origin_server_name = "huly.home.goodkind.io"
        }
      },
      ], [
      for hostname in concat(["tack.goodkind.io"], sort(keys(module.contracts.tack_domain_aliases))) : {
        hostname = hostname
        service  = "https://localhost"
        origin_request = {
          http2_origin       = true
          http_host_header   = "tack.home.goodkind.io"
          origin_server_name = "tack.home.goodkind.io"
        }
      }
      ], [
      { service = "http_status:404" },
    ])
  }
}

import {
  to = cloudflare_zero_trust_tunnel_cloudflared_config.proxy
  id = "${var.cloudflare_account_id}/${module.contracts.tack_tunnel_id}"
}
