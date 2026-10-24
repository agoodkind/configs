resource "pveguest_host_kernel_modules" "poweredge" {
  node    = local.poweredge_node
  modules = local.poweredge_kernel_modules
}

module "poweredge_lxc" {
  source   = "./lxc"
  for_each = local.poweredge_guests

  node                 = local.poweredge_node
  guest                = each.value
  template_file_id     = local.poweredge_template_file_id
  datastore_id         = local.poweredge_datastore_id
  forbidden_interfaces = each.key == "gateway" ? toset(["nic0", "nic3", "ens1f1"]) : toset([])
  restart_on           = each.key == "lan" ? {} : { release = local.poweredge_mwan_release }

  depends_on = [pveguest_host_kernel_modules.poweredge]
}

module "poweredge_mwan" {
  source   = "./mwan"
  for_each = local.poweredge_mwan_roles

  node = local.poweredge_node
  vmid = each.key == "failover" ? module.poweredge_failover_services.vmid : module.poweredge_lxc[each.key].vmid

  role                       = each.value.role
  architecture               = "amd64"
  release_version            = local.poweredge_mwan_release
  excluded_units             = each.value.excluded_units
  sysctl_values              = each.value.sysctl_values
  network_json_path          = each.value.network_json_path
  config_toml_path           = each.value.config_toml_path
  container_options          = module.poweredge_lxc[each.key].options
  host_kernel_modules_loaded = pveguest_host_kernel_modules.poweredge.loaded
}

module "poweredge_lan_services" {
  source = "./poweredge/lan"

  node         = local.poweredge_node
  vmid         = module.poweredge_lxc["lan"].vmid
  nat64_prefix = local.poweredge_nat64_prefix
}

module "poweredge_failover_services" {
  source = "./poweredge/failover"

  node = local.poweredge_node
  vmid = module.poweredge_lxc["failover"].vmid
}
