locals {
  poweredge_failover_vmid = 316
  poweredge_failover_container_options = {
    hostnic0 = "link=nic1v3,name=eth0"
    hostnic1 = "link=nic1v4,name=eth1"
  }
  poweredge_failover_files = {
    sonic = {
      path    = "/etc/systemd/network/10-sonic.network"
      content = file("${path.module}/poweredge/failover/10-sonic.network")
    }
    transit = {
      path    = "/etc/systemd/network/20-transit.network"
      content = file("${path.module}/poweredge/failover/20-transit.network")
    }
    forwarding = {
      path    = "/etc/sysctl.d/91-mwan-failover.conf"
      content = file("${path.module}/../../mwan-failover/sysctl.conf")
    }
  }
}

resource "proxmox_virtual_environment_container" "poweredge_failover" {
  node_name = local.poweredge_node
  vm_id     = local.poweredge_failover_vmid

  initialization {
    hostname = "mwan-failover-poweredge"
  }

  features {
    nesting = true
  }

  disk {
    datastore_id = local.poweredge_datastore_id
    size         = 4
  }

  memory {
    dedicated = 512
    swap      = 0
  }

  cpu {
    architecture = "amd64"
    cores        = 2
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
    ignore_changes = [operating_system[0].template_file_id, started]
  }
}

resource "pveguest_container_options" "poweredge_failover" {
  node    = local.poweredge_node
  vmid    = proxmox_virtual_environment_container.poweredge_failover.vm_id
  options = local.poweredge_failover_container_options

  depends_on = [pveguest_host_kernel_modules.poweredge]
}

resource "pveguest_container_power" "poweredge_failover" {
  node    = local.poweredge_node
  vmid    = proxmox_virtual_environment_container.poweredge_failover.vm_id
  running = true

  restart_on = {
    options = jsonencode(pveguest_container_options.poweredge_failover.options)
    release = local.poweredge_gateway_release_version
  }
}

resource "pveguest_file" "poweredge_failover" {
  for_each = local.poweredge_failover_files

  node = local.poweredge_node
  vmid = pveguest_container_power.poweredge_failover.vmid
  kind = "lxc"

  path    = each.value.path
  content = each.value.content
  mode    = "0644"
}

resource "pveguest_systemd_unit" "poweredge_failover_sysctl" {
  node = local.poweredge_node
  vmid = pveguest_container_power.poweredge_failover.vmid
  kind = "lxc"

  name    = "systemd-sysctl.service"
  enabled = false
  active  = true
  restart_on = {
    forwarding = pveguest_file.poweredge_failover["forwarding"].write_id
  }
}

resource "pveguest_systemd_unit" "poweredge_failover_network" {
  node = local.poweredge_node
  vmid = pveguest_container_power.poweredge_failover.vmid
  kind = "lxc"

  name    = "systemd-networkd.service"
  enabled = true
  active  = true
  restart_on = {
    sonic   = pveguest_file.poweredge_failover["sonic"].write_id
    transit = pveguest_file.poweredge_failover["transit"].write_id
  }

  depends_on = [pveguest_systemd_unit.poweredge_failover_sysctl]
}

resource "pveguest_apt_packages" "poweredge_failover" {
  node = local.poweredge_node
  vmid = pveguest_container_power.poweredge_failover.vmid
  kind = "lxc"

  packages = toset(["ca-certificates", "curl", "ethtool", "iputils-ping", "nftables"])

  depends_on = [pveguest_systemd_unit.poweredge_failover_network]
}

resource "pveguest_file" "poweredge_failover_firewall" {
  node = local.poweredge_node
  vmid = pveguest_container_power.poweredge_failover.vmid
  kind = "lxc"

  path     = "/etc/nftables.conf"
  content  = file("${path.module}/../../mwan-failover/nftables.conf")
  mode     = "0644"
  validate = "/usr/sbin/nft --check --file %s"

  depends_on = [pveguest_apt_packages.poweredge_failover]
}

resource "pveguest_systemd_unit" "poweredge_failover_firewall" {
  node = local.poweredge_node
  vmid = pveguest_container_power.poweredge_failover.vmid
  kind = "lxc"

  name    = "nftables.service"
  enabled = true
  active  = true
  restart_on = {
    rules = pveguest_file.poweredge_failover_firewall.write_id
  }
}

module "poweredge_failover_mwan" {
  source = "./mwan"

  node = local.poweredge_node
  vmid = pveguest_systemd_unit.poweredge_failover_firewall.vmid

  role            = "failover"
  architecture    = local.poweredge_gateway_architecture
  release_version = local.poweredge_gateway_release_version
  excluded_units  = toset([])

  config_toml_path           = "${path.module}/poweredge/failover/config.toml"
  container_options          = pveguest_container_options.poweredge_failover.options
  host_kernel_modules_loaded = pveguest_host_kernel_modules.poweredge.loaded
}
