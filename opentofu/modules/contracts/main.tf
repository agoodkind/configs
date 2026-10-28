variable "repository_root" { type = string }

locals {
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
  host_networks = merge({
    "isp_astound_suburban"   = "vmbr7"
    "isp_att_suburban"       = "vmbr5"
    "isp_mbrains_suburban"   = "vmbr6"
    "isp_routed_suburban"    = "vmbr8"
    "isp_webpass_suburban"   = "vmbr4"
    "mwan_suburban"          = "vmbr2"
    "trunk_suburban"         = "vmbrtrunk"
    "vm_management_suburban" = "vmbr1"
  }, { for name, network in local.routing_inventory.testbed_routing_networks : name => network.bridge })
  cloudflare_zone_ids = {
    "goodkind.io"      = "13ccc5ba064b1661495c0ec86511e9a7"
    "silver-flare.com" = "de02872854ceebbe1d731e3aef200de2"
  }
  poweredge_kernel_modules = toset([
    "nf_tables", "nfnetlink", "nf_conntrack", "nf_nat", "nft_chain_nat",
    "nft_masq", "nft_ct", "nft_nat", "nf_defrag_ipv4", "nf_defrag_ipv6",
    "nft_log", "nf_log_syslog", "nft_limit", "nft_numgen", "nft_hash",
    "sch_ingress", "cls_bpf", "8021q", "sit",
  ])
}

output "host_networks" { value = local.host_networks }

output "cloudflare_zone_ids" { value = local.cloudflare_zone_ids }

output "poweredge_kernel_modules" { value = local.poweredge_kernel_modules }
