locals {
  suburban_hypervisor = yamldecode(
    file("${path.module}/../ansible/inventory/group_vars/all/service_mapping.yml")
  ).service_mapping.suburban_hypervisor

  overlay_hosts = {
    suburban = {
      ssh_host       = local.suburban_hypervisor.ipv6
      node_name      = "hypervisor"
      commit         = "5281556ce105a6f8a0b4e99239e58944ecdf1909"
      guest_api      = true
      kernel_modules = null

      container_options = null

      grub_cmdline_linux_default = null
      network_interfaces_file    = null
    }
    poweredge = {
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
        ]
      }
      container_options = {
        retained_roles = ["PVEAdmin"]
        hostnic = {
          vmids = [313, 314]
          links = ["nic2", "nic1v0", "nic1v1"]
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
      network_interfaces_file    = "${path.module}/overlay/files/poweredge/interfaces"
    }
    vault = {
      ssh_host       = "hypervisor.home.goodkind.io"
      node_name      = "vault"
      commit         = "662112b62488c8acab16cfe26c7297b6c8f95093"
      guest_api      = true
      kernel_modules = null

      container_options = null

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

  container_options = each.value.container_options

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
