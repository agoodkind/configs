removed {
  from = proxmox_sdn_applier.vault
  lifecycle { destroy = false }
}

removed {
  from = proxmox_sdn_applier.vault_finalizer
  lifecycle { destroy = false }
}

removed {
  from = proxmox_sdn_zone_simple.vault
  lifecycle { destroy = false }
}

removed {
  from = proxmox_sdn_vnet.vault
  lifecycle { destroy = false }
}

removed {
  from = proxmox_sdn_subnet.vault_ipv4
  lifecycle { destroy = false }
}

removed {
  from = proxmox_sdn_subnet.vault_ipv6
  lifecycle { destroy = false }
}
