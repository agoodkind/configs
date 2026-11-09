resource "pveguest_file" "ledger_ca" {
  node    = var.guest.node
  vmid    = proxmox_virtual_environment_vm.server.vm_id
  kind    = "qemu"
  path    = "/etc/tack/runtime/ledger-certs/ca.crt"
  mode    = "0644"
  content = var.application.ledger_ca_pem

  depends_on = [module.base]
}

resource "pveguest_file" "application_tmpfiles" {
  node    = var.guest.node
  vmid    = proxmox_virtual_environment_vm.server.vm_id
  kind    = "qemu"
  path    = "/etc/tmpfiles.d/tack-app.conf"
  content = file("${path.module}/files/tack-app.tmpfiles")

  depends_on = [module.base]
}

resource "pveguest_file" "application_files_service" {
  node    = var.guest.node
  vmid    = proxmox_virtual_environment_vm.server.vm_id
  kind    = "qemu"
  path    = "/etc/systemd/system/tack-app-files.service"
  content = file("${path.module}/files/tack-app-files.service")

  depends_on = [module.base]
}

resource "pveguest_file" "application_files_order" {
  node    = var.guest.node
  vmid    = proxmox_virtual_environment_vm.server.vm_id
  kind    = "qemu"
  path    = "/etc/systemd/system/k3s.service.d/tack-app-files.conf"
  content = file("${path.module}/files/tack-app-files.conf")

  depends_on = [module.base]
}

resource "pveguest_systemd_unit" "application_files" {
  node    = var.guest.node
  vmid    = proxmox_virtual_environment_vm.server.vm_id
  kind    = "qemu"
  name    = "tack-app-files.service"
  enabled = true
  active  = true

  restart_on = {
    configuration = pveguest_file.application_tmpfiles.write_id
    service       = pveguest_file.application_files_service.write_id
    bootstrap     = pveguest_file.application_secret["fdb_bootstrap"].write_id
  }
}
