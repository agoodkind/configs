# The Proxmox overlay and its scoped roles on each hypervisor. The automation
# token then manages ACME and the other root-only operations without a root
# login.
locals {
  suburban_hypervisor = yamldecode(
    file("${path.module}/../ansible/inventory/group_vars/all/service_mapping.yml")
  ).service_mapping.suburban_hypervisor

  overlay_hosts = {
    suburban = {
      ssh_host  = local.suburban_hypervisor.ipv6
      node_name = "hypervisor"
    }
    poweredge = {
      ssh_host  = "poweredge.home.goodkind.io"
      node_name = "poweredge"
    }
    vault = {
      ssh_host  = "hypervisor.home.goodkind.io"
      node_name = "vault"
    }
  }
}

module "overlay" {
  source   = "./overlay"
  for_each = local.overlay_hosts

  ssh_host        = each.value.ssh_host
  node_name       = each.value.node_name
  automation_user = local.shared_vars.proxmox_api_user
  acme_account    = local.acme_account
  acme_plugin     = local.acme_plugin
}

# The suburban overlay was declared inside the suburban module.
moved {
  from = module.suburban.remote_file.proxmox_overlays
  to   = module.overlay["suburban"].remote_file.files
}

moved {
  from = module.suburban.terraform_data.proxmox_overlays_apply
  to   = module.overlay["suburban"].terraform_data.apply
}
