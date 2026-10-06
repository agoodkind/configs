locals {
  suburban_hypervisor = yamldecode(
    file("${path.module}/../ansible/inventory/group_vars/all/service_mapping.yml")
  ).service_mapping.suburban_hypervisor

  overlay_hosts = {
    suburban = {
      ssh_host  = local.suburban_hypervisor.ipv6
      node_name = "hypervisor"
      commit    = "bb4324db0f2bf3caef9cd5569e1b3b0afb411aea"
      guest_api = false
    }
    poweredge = {
      ssh_host  = "poweredge.home.goodkind.io"
      node_name = "poweredge"
      commit    = "dafd0319fda87ee71694d371213531d151ba9249"
      guest_api = true
    }
    vault = {
      ssh_host  = "hypervisor.home.goodkind.io"
      node_name = "vault"
      commit    = "bb4324db0f2bf3caef9cd5569e1b3b0afb411aea"
      guest_api = false
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
  acme_account    = local.acme_account
  acme_plugin     = local.acme_plugin
}

moved {
  from = module.suburban.remote_file.proxmox_overlays
  to   = module.overlay["suburban"].remote_file.files
}

moved {
  from = module.suburban.terraform_data.proxmox_overlays_apply
  to   = module.overlay["suburban"].terraform_data.apply
}
