removed {
  from = cloudflare_zero_trust_tunnel_cloudflared_config.proxy

  lifecycle {
    destroy = false
  }
}

variable "import_proxy_retirement" {
  description = "Import the existing proxy tunnel and private routes before the retirement apply."
  type        = bool
  default     = false
}

locals {
  retired_proxy_tunnel_id = "4b602332-6413-4f95-8874-561ed6d9b266"
  retired_proxy_networks = var.import_proxy_retirement ? {
    "10.250.0.110/32" = {
      id      = "15f99199-a175-4e64-a2f6-ed2ab044d0f0"
      comment = "proxy-v4-legacy"
    }
    "3d06:bad:b01::110/128" = {
      id      = "b2ec0ff8-faff-4a31-96f7-1eb67c289a8a"
      comment = "3d06:bad:b01::110/128"
    }
  } : {}
}

resource "cloudflare_zero_trust_tunnel_cloudflared" "retired_proxy" {
  provider = cloudflare.tunnels
  count    = var.import_proxy_retirement ? 1 : 0

  account_id = var.cloudflare_account_id
  name       = "home-proxy"
  config_src = "cloudflare"
}

import {
  for_each = var.import_proxy_retirement ? toset([local.retired_proxy_tunnel_id]) : toset([])
  to       = cloudflare_zero_trust_tunnel_cloudflared.retired_proxy[0]
  id       = "${var.cloudflare_account_id}/${each.key}"
}

resource "cloudflare_zero_trust_tunnel_cloudflared_route" "retired_proxy" {
  provider = cloudflare.tunnel_routes
  for_each = local.retired_proxy_networks

  account_id         = var.cloudflare_account_id
  network            = each.key
  comment            = each.value.comment
  tunnel_id          = cloudflare_zero_trust_tunnel_cloudflared.retired_proxy[0].id
  virtual_network_id = "8a034fb6-d06e-4c08-a567-7860e351635e"
}

import {
  for_each = local.retired_proxy_networks
  to       = cloudflare_zero_trust_tunnel_cloudflared_route.retired_proxy[each.key]
  id       = "${var.cloudflare_account_id}/${each.value.id}"
}
