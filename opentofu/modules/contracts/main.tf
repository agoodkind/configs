variable "repository_root" { type = string }

locals {
  tack_tunnel_id = "4b602332-6413-4f95-8874-561ed6d9b266"

  shared_vars       = yamldecode(file("${var.repository_root}/ansible/inventory/group_vars/all/vars.yml"))
  routing_inventory = yamldecode(file("${var.repository_root}/ansible/inventory/group_vars/all/service_mapping.yml"))
  service_mapping   = local.routing_inventory.service_mapping
  proxy_vars        = yamldecode(file("${var.repository_root}/ansible/inventory/group_vars/proxy_servers.yml"))
}

output "shared_vars" { value = local.shared_vars }

output "service_mapping" { value = local.service_mapping }

output "routing_inventory" { value = local.routing_inventory }

output "proxy_vars" { value = local.proxy_vars }

output "proxmox_token_principal" { value = "${local.shared_vars.proxmox_api_user}!${local.shared_vars.proxmox_token_id}" }

locals {
  tack_domain_aliases = {
    "get-tack.com"    = "cfff9eaba9a95c6b6375be80bb88a31c"
    "tack.so"         = "ab327a658a8a5c849315a2833c6ae007"
    "tack.technology" = "6b20dd82026942a481429af4447d75f2"
  }

  cloudflare_zone_ids = {
    "goodkind.io"      = "13ccc5ba064b1661495c0ec86511e9a7"
    "silver-flare.com" = "de02872854ceebbe1d731e3aef200de2"
  }
}

output "cloudflare_zone_ids" { value = local.cloudflare_zone_ids }

output "tack_domain_aliases" { value = local.tack_domain_aliases }

output "tack_tunnel_id" { value = local.tack_tunnel_id }
