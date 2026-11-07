module "contracts" { source = "../../../contracts" }

locals {
  proxmox_nodes = {
    suburban = {
      endpoint  = "https://[${local.service_mapping.suburban_hypervisor.ipv6}]:8006"
      insecure  = true
      node_name = "hypervisor"
    }
  }

  docker_guests = toset(["tack_app2_suburban"])
}

output "proxmox_nodes" { value = local.proxmox_nodes }

output "docker_bridges" {
  value = {
    for key in local.docker_guests : key => merge(local.proxmox_nodes[local.service_mapping[key].node], {
      node            = local.service_mapping[key].node
      vmid            = local.service_mapping[key].vmid
      kind            = lookup(local.service_mapping[key], "kind", "lxc")
      token_principal = module.contracts.proxmox_token_principal
      token_variable  = "vault_suburban_testbed_pve_token_secret"
    })
  }
}
