locals {
  poweredge_node = "poweredge"

  poweredge_kernel_modules = toset([
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
  ])

  poweredge_gateway_vmid = 313

  poweredge_gateway_hostname = "mwan-poweredge"

  poweredge_gateway_role              = "wan"
  poweredge_gateway_architecture      = "amd64"
  poweredge_gateway_release_version   = "202610192152-ef-9bc78a1"
  poweredge_gateway_network_json_path = "${path.module}/poweredge/network.json"
  poweredge_gateway_config_toml_path  = "${path.module}/poweredge/config.toml"
  poweredge_gateway_excluded_units    = toset(["nghttpx-wanconfig.service", "rousette.service"])

  poweredge_template_file_id = "local:vztmpl/debian-13-standard_13.6-1_amd64.tar.zst"
  poweredge_datastore_id     = "vmdata"

  poweredge_gateway_size = {
    cpu_cores    = 16
    memory_mb    = 2048
    swap_mb      = 512
    disk_size_gb = 8
  }

  # The gateway must not own LAN-facing ports.
  poweredge_lan_ports = toset(["nic0", "nic3", "ens1f1"])

  poweredge_gateway_container_options = {
    hostnic0    = "link=nic1v0,name=sonic"
    hostnic1    = "link=nic2v0,name=etheric"
    hostnic2    = "link=nic1v1,name=transit"
    bpfdelegate = "cmds=map_create;prog_load;btf_load,maps=hash,progs=socket_filter;sched_cls,attachs=cgroup_inet_ingress;tcx_ingress;tcx_egress"
  }

  poweredge_container_options = {
    gateway = local.poweredge_gateway_container_options
  }

  poweredge_container_hostnic_links = {
    for name, options in local.poweredge_container_options : name => [
      for key, value in options :
      regex("(?:^|,)link=([^,]+)", value)[0]
      if startswith(key, "hostnic")
    ]
  }

  poweredge_container_lan_links = {
    for name, links in local.poweredge_container_hostnic_links : name => [
      for link in links : link
      if contains(local.poweredge_lan_ports, link)
    ]
  }
}

resource "pveguest_host_kernel_modules" "poweredge" {
  node    = local.poweredge_node
  modules = local.poweredge_kernel_modules
}

resource "proxmox_virtual_environment_container" "poweredge_gateway" {
  node_name = local.poweredge_node
  vm_id     = local.poweredge_gateway_vmid

  initialization {
    hostname = local.poweredge_gateway_hostname
  }

  features {
    nesting = true
  }

  disk {
    datastore_id = local.poweredge_datastore_id
    size         = local.poweredge_gateway_size.disk_size_gb
  }

  memory {
    dedicated = local.poweredge_gateway_size.memory_mb
    swap      = local.poweredge_gateway_size.swap_mb
  }

  cpu {
    architecture = "amd64"
    cores        = local.poweredge_gateway_size.cpu_cores
    limit        = 0
  }

  operating_system {
    template_file_id = local.poweredge_template_file_id
    type             = "debian"
  }

  started       = false
  start_on_boot = false
  unprivileged  = true

  lifecycle {
    ignore_changes = [
      operating_system[0].template_file_id,
      started,
    ]

    postcondition {
      condition     = length(self.network_interface) == 0
      error_message = "The following container must receive host interfaces only through hostnicN options and declare no network_interface blocks: ${local.poweredge_gateway_hostname}"
    }
  }
}

removed {
  from = proxmox_virtual_environment_container.poweredge_lan
  lifecycle {
    destroy = false
  }
}

resource "pveguest_container_options" "poweredge_gateway" {
  node    = local.poweredge_node
  vmid    = proxmox_virtual_environment_container.poweredge_gateway.vm_id
  options = local.poweredge_gateway_container_options

  lifecycle {
    precondition {
      condition     = length(local.poweredge_container_lan_links.gateway) == 0
      error_message = "The PowerEdge WAN container cannot own LAN-facing ports (nic0, nic3, ens1f1): ${join(", ", local.poweredge_container_lan_links.gateway)}"
    }
  }

  depends_on = [pveguest_host_kernel_modules.poweredge]
}

removed {
  from = pveguest_container_options.poweredge_lan
  lifecycle {
    destroy = false
  }
}

resource "pveguest_container_power" "poweredge_gateway" {
  node    = local.poweredge_node
  vmid    = proxmox_virtual_environment_container.poweredge_gateway.vm_id
  running = true

  # A restart clears rules loaded before nftables.service was masked.
  restart_on = {
    options = jsonencode(pveguest_container_options.poweredge_gateway.options)
    release = local.poweredge_gateway_release_version
  }

  depends_on = [
    pveguest_container_options.poweredge_gateway,
    pveguest_host_kernel_modules.poweredge,
  ]
}

removed {
  from = pveguest_container_power.poweredge_lan
  lifecycle {
    destroy = false
  }
}

module "poweredge_gateway_mwan" {
  source = "./mwan"

  node = local.poweredge_node
  vmid = pveguest_container_power.poweredge_gateway.vmid

  role            = local.poweredge_gateway_role
  architecture    = local.poweredge_gateway_architecture
  release_version = local.poweredge_gateway_release_version
  excluded_units  = local.poweredge_gateway_excluded_units

  network_json_path          = local.poweredge_gateway_network_json_path
  config_toml_path           = local.poweredge_gateway_config_toml_path
  container_options          = pveguest_container_options.poweredge_gateway.options
  host_kernel_modules_loaded = pveguest_host_kernel_modules.poweredge.loaded
}

removed {
  from = module.poweredge_lan_router
  lifecycle {
    destroy = false
  }
}
