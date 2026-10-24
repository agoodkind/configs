import {
  to = module.poweredge_lxc["lan"].proxmox_virtual_environment_container.this
  id = "poweredge/314"
}

moved {
  from = proxmox_virtual_environment_container.poweredge_gateway
  to   = module.poweredge_lxc["gateway"].proxmox_virtual_environment_container.this
}

moved {
  from = pveguest_container_options.poweredge_gateway
  to   = module.poweredge_lxc["gateway"].pveguest_container_options.this
}

moved {
  from = pveguest_container_power.poweredge_gateway
  to   = module.poweredge_lxc["gateway"].pveguest_container_power.this
}

moved {
  from = proxmox_virtual_environment_container.poweredge_failover
  to   = module.poweredge_lxc["failover"].proxmox_virtual_environment_container.this
}

moved {
  from = pveguest_container_options.poweredge_failover
  to   = module.poweredge_lxc["failover"].pveguest_container_options.this
}

moved {
  from = pveguest_container_power.poweredge_failover
  to   = module.poweredge_lxc["failover"].pveguest_container_power.this
}

moved {
  from = proxmox_virtual_environment_container.poweredge_lan
  to   = module.poweredge_lxc["lan"].proxmox_virtual_environment_container.this
}

moved {
  from = pveguest_container_options.poweredge_lan
  to   = module.poweredge_lxc["lan"].pveguest_container_options.this
}

moved {
  from = pveguest_container_power.poweredge_lan
  to   = module.poweredge_lxc["lan"].pveguest_container_power.this
}

moved {
  from = module.poweredge_gateway_mwan
  to   = module.poweredge_mwan["gateway"]
}

moved {
  from = module.poweredge_failover_mwan
  to   = module.poweredge_mwan["failover"]
}

moved {
  from = pveguest_file.poweredge_failover
  to   = module.poweredge_failover_services.pveguest_file.config
}

moved {
  from = pveguest_file.poweredge_failover_firewall
  to   = module.poweredge_failover_services.pveguest_file.firewall
}

moved {
  from = pveguest_systemd_unit.poweredge_failover_sysctl
  to   = module.poweredge_failover_services.pveguest_systemd_unit.sysctl
}

moved {
  from = pveguest_systemd_unit.poweredge_failover_network
  to   = module.poweredge_failover_services.pveguest_systemd_unit.network
}

moved {
  from = pveguest_systemd_unit.poweredge_failover_firewall
  to   = module.poweredge_failover_services.pveguest_systemd_unit.firewall
}

moved {
  from = pveguest_apt_packages.poweredge_failover
  to   = module.poweredge_failover_services.pveguest_apt_packages.this
}

removed {
  from = module.poweredge_lan_router
  lifecycle {
    destroy = false
  }
}
