module "poweredge_hypervisor" { source = "../../services/poweredge_hypervisor" }
locals {
  service_mapping = {
    poweredge_hypervisor = module.poweredge_hypervisor.value
  }
}
output "service_mapping" { value = local.service_mapping }
locals {
  lock_targets = {
    "module_hosts" = {
      "module.poweredge_failover_services"  = "poweredge"
      "module.poweredge_lan_services"       = "poweredge"
      "module.poweredge_lxc[\"failover\"]"  = "poweredge"
      "module.poweredge_lxc[\"gateway\"]"   = "poweredge"
      "module.poweredge_lxc[\"lan\"]"       = "poweredge"
      "module.poweredge_mwan[\"failover\"]" = "poweredge"
      "module.poweredge_mwan[\"gateway\"]"  = "poweredge"
    }
    "node_hosts" = {
      "poweredge" = "poweredge"
    }
    "lock_free_types" = ["mwan_network_config", "terraform_data"]
  }
}
module "poweredge_host" { source = "../.." }
output "lock_targets" { value = merge(local.lock_targets, { lock_endpoints = {
  "poweredge" = module.poweredge_host.lock_endpoint
} }) }
