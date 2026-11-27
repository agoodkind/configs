removed {
  from = proxmox_virtual_environment_container.unifi
  lifecycle { destroy = false }
}

removed {
  from = module.guest_network
  lifecycle { destroy = false }
}
