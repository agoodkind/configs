locals {
  suburban_hypervisor = yamldecode(
    file("${path.module}/../ansible/inventory/group_vars/all/service_mapping.yml")
  ).service_mapping.suburban_hypervisor

  overlay_hosts = {
    suburban = {
      ssh_host       = local.suburban_hypervisor.ipv6
      node_name      = "hypervisor"
      commit         = "dafd0319fda87ee71694d371213531d151ba9249"
      guest_api      = true
      kernel_modules = null

      grub_cmdline_linux_default = null
      network_interfaces_file    = null
    }
    poweredge = {
      ssh_host  = "poweredge.home.goodkind.io"
      node_name = "poweredge"
      commit    = "d70783290018a21460f0607fc03b5c5128f840a0"
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
        ]
      }
      grub_cmdline_linux_default = "systemd.show_status=1"
      network_interfaces_file    = "${path.module}/overlay/files/poweredge/interfaces"
    }
    vault = {
      ssh_host       = "hypervisor.home.goodkind.io"
      node_name      = "vault"
      commit         = "662112b62488c8acab16cfe26c7297b6c8f95093"
      guest_api      = true
      kernel_modules = null

      grub_cmdline_linux_default = null
      network_interfaces_file    = null
    }
  }
}

module "overlay" {
  source   = "./overlay"
  for_each = local.overlay_hosts

  ssh_host        = each.value.ssh_host
  node_name       = each.value.node_name
  commit          = each.value.commit
  guest_api       = each.value.guest_api
  automation_user = local.shared_vars.proxmox_api_user
  kernel_modules  = each.value.kernel_modules
  acme_account    = local.acme_account
  acme_plugin     = local.acme_plugin

  grub_cmdline_linux_default = each.value.grub_cmdline_linux_default
  network_interfaces_file    = each.value.network_interfaces_file
}

moved {
  from = module.suburban.remote_file.proxmox_overlays
  to   = module.overlay["suburban"].remote_file.files
}

moved {
  from = module.suburban.terraform_data.proxmox_overlays_apply
  to   = module.overlay["suburban"].terraform_data.apply
}
