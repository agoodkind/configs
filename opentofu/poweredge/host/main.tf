locals { repository_root = abspath("${path.module}/../../..") }

module "contracts" {
  source          = "../../modules/contracts"
  repository_root = local.repository_root
}

locals {
  shared_vars         = module.contracts.shared_vars
  service_mapping     = module.contracts.service_mapping
  cloudflare_zone_ids = module.contracts.cloudflare_zone_ids
}

resource "proxmox_acme_account" "poweredge" {
  provider  = proxmox.poweredge
  name      = local.acme_account
  contact   = var.cloudflare_owner_email
  directory = local.acme_directory
  tos       = local.acme_terms

  depends_on = [module.overlay]
}

resource "proxmox_acme_certificate" "poweredge" {
  provider  = proxmox.poweredge
  node_name = "poweredge"
  account   = proxmox_acme_account.poweredge.name

  domains = [
    {
      domain = "poweredge.home.goodkind.io"
      plugin = proxmox_acme_dns_plugin.poweredge.plugin
    }
  ]
  depends_on = [module.overlay]
}

resource "proxmox_acme_dns_plugin" "poweredge" {
  provider = proxmox.poweredge
  plugin   = local.acme_plugin
  api      = "cf"

  data_wo = {
    CF_Account_ID = var.cloudflare_account_id
    CF_Zone_ID    = local.cloudflare_zone_ids["goodkind.io"]
    CF_Token      = var.vault_poweredge_acme_cloudflare_token
  }
  data_wo_version = local.acme_plugin_data_version

  depends_on = [module.overlay]
}

resource "pveguest_host_kernel_modules" "poweredge" {
  node    = local.poweredge_node
  modules = local.poweredge_kernel_modules
}

locals {

  overlay_hosts = { poweredge = {
    ssh_host  = "poweredge.home.goodkind.io"
    node_name = "poweredge"
    commit    = "662112b62488c8acab16cfe26c7297b6c8f95093"
    guest_api = true
    kernel_modules = {
      allow = [
        "nf_tables",
        "nfnetlink",
        "nf_conntrack",
        "nf_nat",
        "nft_chain_nat",
        "nft_masq",
        "nft_ct",
        "nft_nat",
        "nf_defrag_ipv4",
        "nf_defrag_ipv6",
        "nft_log",
        "nf_log_syslog",
        "nft_limit",
        "nft_numgen",
        "nft_hash",
        "sch_ingress",
        "cls_bpf",
        "8021q",
        "sit",
      ]
    }
    container_options = {
      retained_roles = ["PVEAdmin"]
      hostnic = {
        vmids = [313, 314, 316]
        links = ["nic0v1", "nic1v0", "nic1v1", "nic1v2", "nic1v3", "nic1v4", "nic2v0", "nic3v0"]
      }
      bpfdelegate = {
        vmids   = [313]
        cmds    = ["map_create", "prog_load", "btf_load"]
        maps    = ["hash"]
        progs   = ["socket_filter", "sched_cls"]
        attachs = ["cgroup_inet_ingress", "tcx_ingress", "tcx_egress"]
      }
    }
    grub_cmdline_linux_default = "systemd.show_status=1"
    network_interfaces_file    = "${path.module}/../../modules/overlay/files/poweredge/interfaces"
  } }
}

module "overlay" {
  source   = "../../modules/overlay"
  for_each = local.overlay_hosts

  ssh_host        = each.value.ssh_host
  node_name       = each.value.node_name
  commit          = each.value.commit
  guest_api       = each.value.guest_api
  automation_user = local.shared_vars.proxmox_api_user
  kernel_modules  = each.value.kernel_modules
  acme_account    = local.acme_account
  acme_plugin     = local.acme_plugin

  container_options = each.value.container_options

  grub_cmdline_linux_default = each.value.grub_cmdline_linux_default
  network_interfaces_file    = each.value.network_interfaces_file
}

# Each ACME account and DNS plugin depends on module.overlay. The automation
# token of the provider needs the Sys.ACME roles that module.overlay grants.
#
# `tofu test` fails on an import block. Import an existing account with id
# "default", a plugin with id "cf", and a certificate with its node name
# through `./configsctl tofu import`.
locals {
  acme_directory = "https://acme-v02.api.letsencrypt.org/directory"
  acme_terms     = "https://letsencrypt.org/documents/LE-SA-v1.5-February-24-2025.pdf"
  acme_account   = "default"
  acme_plugin    = "cf"

  # OpenTofu sends data_wo to Proxmox again only when this number increases.
  # Increase it after a change of a Cloudflare DNS token.
  acme_plugin_data_version = 2
}

locals {
  poweredge_node = "poweredge"

  poweredge_kernel_modules = module.contracts.poweredge_kernel_modules


}

locals {
  nodes = {
    poweredge = {
      endpoint  = "https://${local.service_mapping.poweredge_hypervisor.ipv4}:8006"
      api_token = "${local.proxmox_token_principal}=${var.vault_poweredge_pve_token_secret}"
      insecure  = true
    }
  }
}

locals { proxmox_token_principal = module.contracts.proxmox_token_principal }
