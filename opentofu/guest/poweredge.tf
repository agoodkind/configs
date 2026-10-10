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
  ])

  poweredge_gateway_vmid = 313
  poweredge_lan_vmid     = 314

  poweredge_gateway_hostname = "mwan-poweredge"
  poweredge_lan_hostname     = "lan-poweredge"

  poweredge_gateway_role              = "wan"
  poweredge_gateway_architecture      = "amd64"
  poweredge_gateway_release_version   = "202610101541-d2-4aa6e92"
  poweredge_gateway_network_json_path = "${path.module}/poweredge/network.json"
  poweredge_gateway_config_toml_path  = "${path.module}/poweredge/config.toml"
  poweredge_gateway_excluded_units    = toset(["nghttpx-wanconfig.service"])

  poweredge_template_file_id = "local:vztmpl/debian-13-standard_13.6-1_amd64.tar.zst"
  poweredge_datastore_id     = "vmdata"

  poweredge_gateway_size = {
    cpu_cores    = 2
    memory_mb    = 2048
    swap_mb      = 512
    disk_size_gb = 8
  }

  poweredge_lan_size = {
    cpu_cores    = 2
    memory_mb    = 1024
    swap_mb      = 512
    disk_size_gb = 8
  }

  # Keep LAN client ports on the host until the user authorizes LAN activation.
  poweredge_lan_ports = toset(["nic0", "nic3", "ens1f1"])

  poweredge_gateway_container_options = {
    hostnic0    = "link=nic2,name=wan"
    hostnic1    = "link=nic1v0,name=mwanbr"
    bpfdelegate = "cmds=map_create;prog_load;btf_load,maps=hash,progs=socket_filter;sched_cls,attachs=cgroup_inet_ingress;tcx_ingress;tcx_egress"
  }

  poweredge_lan_container_options = {
    hostnic0 = "link=nic1v1,name=mwanbr"
  }

  poweredge_container_options = {
    gateway = local.poweredge_gateway_container_options
    lan     = local.poweredge_lan_container_options
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
    ]

    postcondition {
      condition     = length(self.network_interface) == 0
      error_message = "The following container must receive host interfaces only through hostnicN options and declare no network_interface blocks: ${local.poweredge_gateway_hostname}"
    }
  }
}

resource "proxmox_virtual_environment_container" "poweredge_lan" {
  node_name = local.poweredge_node
  vm_id     = local.poweredge_lan_vmid

  initialization {
    hostname = local.poweredge_lan_hostname
  }

  features {
    nesting = true
  }

  disk {
    datastore_id = local.poweredge_datastore_id
    size         = local.poweredge_lan_size.disk_size_gb
  }

  memory {
    dedicated = local.poweredge_lan_size.memory_mb
    swap      = local.poweredge_lan_size.swap_mb
  }

  cpu {
    architecture = "amd64"
    cores        = local.poweredge_lan_size.cpu_cores
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
    ]

    postcondition {
      condition     = length(self.network_interface) == 0
      error_message = "The following container must receive host interfaces only through hostnicN options and declare no network_interface blocks: ${local.poweredge_lan_hostname}"
    }
  }
}

resource "pveguest_container_options" "poweredge_gateway" {
  node    = local.poweredge_node
  vmid    = proxmox_virtual_environment_container.poweredge_gateway.vm_id
  options = local.poweredge_gateway_container_options

  lifecycle {
    precondition {
      condition     = length(local.poweredge_container_lan_links.gateway) == 0
      error_message = "Container options assign PowerEdge LAN ports that must stay on the host while the LAN is dormant (nic0, nic3, ens1f1): ${join(", ", local.poweredge_container_lan_links.gateway)}"
    }
  }

  depends_on = [pveguest_host_kernel_modules.poweredge]
}

resource "pveguest_container_options" "poweredge_lan" {
  node    = local.poweredge_node
  vmid    = proxmox_virtual_environment_container.poweredge_lan.vm_id
  options = local.poweredge_lan_container_options

  lifecycle {
    precondition {
      condition     = length(local.poweredge_container_lan_links.lan) == 0
      error_message = "Container options assign PowerEdge LAN ports that must stay on the host while the LAN is dormant (nic0, nic3, ens1f1): ${join(", ", local.poweredge_container_lan_links.lan)}"
    }
  }

  depends_on = [pveguest_host_kernel_modules.poweredge]
}

resource "pveguest_container_power" "poweredge_gateway" {
  node    = local.poweredge_node
  vmid    = proxmox_virtual_environment_container.poweredge_gateway.vm_id
  running = true

  restart_on = {
    options = jsonencode(pveguest_container_options.poweredge_gateway.options)
  }

  depends_on = [
    pveguest_container_options.poweredge_gateway,
    pveguest_host_kernel_modules.poweredge,
  ]
}

resource "pveguest_container_power" "poweredge_lan" {
  node    = local.poweredge_node
  vmid    = proxmox_virtual_environment_container.poweredge_lan.vm_id
  running = true

  restart_on = {
    options = jsonencode(pveguest_container_options.poweredge_lan.options)
  }

  depends_on = [
    pveguest_container_options.poweredge_lan,
    pveguest_host_kernel_modules.poweredge,
  ]
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

module "poweredge_lan_router" {
  source = "./lanrouter"

  node     = local.poweredge_node
  vmid     = pveguest_container_power.poweredge_lan.vmid
  hostname = local.poweredge_lan_hostname

  transit_interface    = "mwanbr"
  transit_ipv4_address = "10.230.230.1/29"
  transit_ipv6_address = "3d06:bad:b01:3f0::1/64"

  bgp_asn           = 4200000001
  peer_ipv4_address = "10.230.230.2"
  peer_ipv6_address = "3d06:bad:b01:3f0::2"

  lan_ipv4_prefixes = ["10.230.1.0/24", "10.230.2.0/24"]
  lan_ipv6_prefixes = ["3d06:bad:b01:301::/64", "3d06:bad:b01:302::/64"]

  acceptance_ipv4_prefixes = ["198.51.100.0/24"]
  acceptance_ipv6_prefixes = ["2001:db8:5::/48"]
}
