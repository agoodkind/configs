removed {
  from = pveguest_host_kernel_modules.poweredge

  lifecycle {
    destroy = false
  }
}

removed {
  from = proxmox_virtual_environment_container.poweredge_gateway

  lifecycle {
    destroy = false
  }
}

removed {
  from = proxmox_virtual_environment_container.poweredge_lan

  lifecycle {
    destroy = false
  }
}

removed {
  from = pveguest_container_options.poweredge_gateway

  lifecycle {
    destroy = false
  }
}

removed {
  from = pveguest_container_options.poweredge_lan

  lifecycle {
    destroy = false
  }
}

removed {
  from = pveguest_container_power.poweredge_gateway

  lifecycle {
    destroy = false
  }
}

removed {
  from = pveguest_container_power.poweredge_lan

  lifecycle {
    destroy = false
  }
}

removed {
  from = module.poweredge_gateway_mwan

  lifecycle {
    destroy = false
  }
}

removed {
  from = module.poweredge_lan_router

  lifecycle {
    destroy = false
  }
}
