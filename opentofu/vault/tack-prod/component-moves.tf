moved {
  from = module.tack_k3s.proxmox_download_file.cloud_image
  to   = module.tack_k3s_vm.proxmox_download_file.cloud_image
}

moved {
  from = module.tack_k3s.proxmox_virtual_environment_file.cloud_init
  to   = module.tack_k3s_vm.proxmox_virtual_environment_file.cloud_init
}

moved {
  from = module.tack_k3s.proxmox_virtual_environment_vm.server
  to   = module.tack_k3s_vm.proxmox_virtual_environment_vm.server
}

moved {
  from = module.tack_k3s.module.base
  to   = module.tack_k3s_base
}

moved {
  from = module.tack_k3s_base.pveguest_systemd_unit.networkd_wait_online[0]
  to   = pveguest_systemd_unit.networkd_wait_online
}

moved {
  from = module.tack_k3s.pveguest_file.application_manifest
  to   = module.tack_application.pveguest_file.application_manifest
}

moved {
  from = module.tack_k3s.pveguest_file.ledger_ca
  to   = module.tack_application.pveguest_file.ledger_ca
}

moved {
  from = module.tack_k3s.pveguest_file.application_tmpfiles
  to   = module.tack_application.pveguest_file.application_tmpfiles
}

moved {
  from = module.tack_k3s.pveguest_file.application_files_service
  to   = module.tack_application.pveguest_file.application_files_service
}

moved {
  from = module.tack_k3s.pveguest_file.application_files_order
  to   = module.tack_application.pveguest_file.application_files_order
}

moved {
  from = module.tack_k3s.pveguest_file.application_secret
  to   = module.tack_application.pveguest_file.application_secret
}

moved {
  from = module.tack_k3s.pveguest_systemd_unit.application_files
  to   = module.tack_application.pveguest_systemd_unit.application_files
}
