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

locals {
  poweredge_node               = "poweredge"
  poweredge_template_file_id   = "local:vztmpl/debian-13-standard_13.6-1_amd64.tar.zst"
  poweredge_datastore_id       = "vmdata"
  poweredge_mwan_release       = "202610192152-ef-9bc78a1"
  poweredge_nat64_prefix       = "3d06:bad:6464::/96"
  poweredge_vpp_idle_wait_usec = 10


  poweredge_guests = {
    gateway = {
      vmid         = 313
      hostname     = "mwan-poweredge"
      cpu_cores    = 16
      memory_mb    = 2048
      disk_size_gb = 8
      devices      = []
      console      = false
      options = {
        hostnic0    = "link=nic1v0,name=sonic"
        hostnic1    = "link=nic2v0,name=etheric"
        hostnic2    = "link=nic1v1,name=transit"
        bpfdelegate = "cmds=map_create;prog_load;btf_load,maps=hash,progs=socket_filter;sched_cls,attachs=cgroup_inet_ingress;tcx_ingress;tcx_egress"
      }
    }
    lan = {
      vmid         = 314
      hostname     = "edge-router"
      cpu_cores    = 16
      memory_mb    = 1024
      disk_size_gb = 8
      devices      = ["/dev/net/tun", "/dev/vhost-net"]
      console      = true
      options = {
        hostnic0 = "link=nic1v2,name=transit"
        hostnic1 = "link=nic3v0,name=lan"
        hostnic2 = "link=nic0v1,name=lan-out,up=0"
      }
    }
    failover = {
      vmid         = 316
      hostname     = "mwan-failover-poweredge"
      cpu_cores    = 2
      memory_mb    = 512
      disk_size_gb = 4
      devices      = []
      console      = false
      options = {
        hostnic0 = "link=nic1v3,name=eth0"
        hostnic1 = "link=nic1v4,name=eth1"
      }
    }
  }

  poweredge_mwan_roles = {
    gateway = {
      role              = "wan"
      network_json_path = "${path.module}/../../modules/poweredge/network.json"
      config_toml_path  = "${path.module}/../../modules/poweredge/config.toml"
      excluded_units    = toset(["nghttpx-wanconfig.service", "rousette.service"])
      sysctl_values     = { "net.ipv4.fwmark_reflect" = "1" }
    }
    failover = {
      role              = "failover"
      network_json_path = null
      config_toml_path  = "${path.module}/../../modules/poweredge/failover/config.toml"
      excluded_units    = toset([])
      sysctl_values     = {}
    }
  }
}

module "poweredge_lxc" {
  source   = "../../modules/lxc"
  for_each = local.poweredge_guests

  node                 = local.poweredge_node
  guest                = each.value
  template_file_id     = local.poweredge_template_file_id
  datastore_id         = local.poweredge_datastore_id
  forbidden_interfaces = each.key == "gateway" ? toset(["nic0", "nic3", "ens1f1"]) : toset([])
  restart_on           = each.key == "lan" ? {} : { release = local.poweredge_mwan_release }

  depends_on = [data.pveguest_host_kernel_modules.poweredge]
}

module "poweredge_mwan" {
  source   = "../../modules/mwan"
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
  host_kernel_modules_loaded = data.pveguest_host_kernel_modules.poweredge.loaded
}

module "poweredge_lan_services" {
  source = "../../modules/poweredge/lan"

  node               = local.poweredge_node
  vmid               = module.poweredge_lxc["lan"].vmid
  nat64_prefix       = local.poweredge_nat64_prefix
  vpp_idle_wait_usec = local.poweredge_vpp_idle_wait_usec
}

module "poweredge_failover_services" {
  source = "../../modules/poweredge/failover"

  node = local.poweredge_node
  vmid = module.poweredge_lxc["failover"].vmid
}

data "pveguest_host_kernel_modules" "poweredge" {
  node = local.poweredge_node
  lifecycle {
    postcondition {
      condition     = alltrue([for module in module.contracts.poweredge_kernel_modules : lookup(self.loaded, module, false)])
      error_message = "PowerEdge has not loaded every kernel module required by its routing guests."
    }
  }
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
