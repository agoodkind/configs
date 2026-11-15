# Forget records on deleted guests without contacting those guests.
removed {
  from = module.base
  lifecycle { destroy = false }
}

removed {
  from = module.suburban
  lifecycle { destroy = false }
}

removed {
  from = module.docker_guest
  lifecycle { destroy = false }
}

removed {
  from = module.tack_release
  lifecycle { destroy = false }
}

removed {
  from = module.tack_release_app
  lifecycle { destroy = false }
}

removed {
  from = pveguest_download.compose_network
  lifecycle { destroy = false }
}

removed {
  from = pveguest_file.compose_network_unit
  lifecycle { destroy = false }
}

removed {
  from = pveguest_file.compose_network_script
  lifecycle { destroy = false }
}

removed {
  from = pveguest_systemd_unit.compose_network
  lifecycle { destroy = false }
}

removed {
  from = pveguest_file.k3s_kafka_overlay
  lifecycle { destroy = false }
}

removed {
  from = pveguest_file.k3s_kafka_firewall
  lifecycle { destroy = false }
}

removed {
  from = pveguest_file.k3s_kafka_unit
  lifecycle { destroy = false }
}

removed {
  from = pveguest_systemd_unit.k3s_kafka_firewall
  lifecycle { destroy = false }
}

removed {
  from = pveguest_systemd_unit.k3s_kafka_listener
  lifecycle { destroy = false }
}

removed {
  from = pveguest_file.legacy_app_kafka_unit
  lifecycle { destroy = false }
}

removed {
  from = pveguest_systemd_unit.legacy_app_kafka
  lifecycle { destroy = false }
}

removed {
  from = module.tack_k3s.pveguest_file.ledger_ca
  lifecycle { destroy = false }
}

removed {
  from = module.tack_k3s.pveguest_file.application_tmpfiles
  lifecycle { destroy = false }
}

removed {
  from = module.tack_k3s.pveguest_file.application_files_service
  lifecycle { destroy = false }
}

removed {
  from = module.tack_k3s.pveguest_file.application_files_order
  lifecycle { destroy = false }
}

removed {
  from = module.tack_k3s.pveguest_systemd_unit.application_files
  lifecycle { destroy = false }
}

removed {
  from = module.tack_k3s.pveguest_file.application_manifest
  lifecycle { destroy = false }
}

removed {
  from = module.tack_k3s.pveguest_file.application_secret
  lifecycle { destroy = false }
}

removed {
  from = module.tack_k3s.pveguest_file.registry
  lifecycle { destroy = false }
}

removed {
  from = module.tack_k3s.pveguest_download.k3s
  lifecycle { destroy = false }
}

removed {
  from = module.tack_k3s.pveguest_file.configuration
  lifecycle { destroy = false }
}

removed {
  from = module.tack_k3s.pveguest_file.server_token
  lifecycle { destroy = false }
}

removed {
  from = module.tack_k3s.pveguest_file.snapshot_credentials
  lifecycle { destroy = false }
}

removed {
  from = module.tack_k3s.pveguest_file.snapshot_ca
  lifecycle { destroy = false }
}

removed {
  from = module.tack_k3s.pveguest_file.service
  lifecycle { destroy = false }
}

removed {
  from = module.tack_k3s.pveguest_systemd_unit.server
  lifecycle { destroy = false }
}

removed {
  from = module.tack_k3s.module.base
  lifecycle { destroy = false }
}

removed {
  from = module.tack_k3s.proxmox_virtual_environment_vm.server
  lifecycle { destroy = false }
}
