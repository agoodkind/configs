resource "pveguest_file" "ledger_ca" {
  node    = var.guest.node
  vmid    = var.guest.vmid
  kind    = "qemu"
  path    = "/etc/tack/runtime/ledger-certs/ca.crt"
  mode    = "0644"
  content = var.application.ledger_ca_pem
}

resource "pveguest_file" "application_tmpfiles" {
  node    = var.guest.node
  vmid    = var.guest.vmid
  kind    = "qemu"
  path    = "/etc/tmpfiles.d/tack-app.conf"
  content = file("${path.module}/files/tack-app.tmpfiles")
}

resource "pveguest_file" "application_files_service" {
  node    = var.guest.node
  vmid    = var.guest.vmid
  kind    = "qemu"
  path    = "/etc/systemd/system/tack-app-files.service"
  content = file("${path.module}/files/tack-app-files.service")
}

resource "pveguest_file" "application_files_order" {
  node    = var.guest.node
  vmid    = var.guest.vmid
  kind    = "qemu"
  path    = "/etc/systemd/system/k3s.service.d/tack-app-files.conf"
  content = file("${path.module}/files/tack-app-files.conf")
}

resource "pveguest_systemd_unit" "application_files" {
  node    = var.guest.node
  vmid    = var.guest.vmid
  kind    = "qemu"
  name    = "tack-app-files.service"
  enabled = true
  active  = true

  restart_on = {
    configuration = pveguest_file.application_tmpfiles.write_id
    service       = pveguest_file.application_files_service.write_id
    bootstrap     = pveguest_file.application_secret["fdb_bootstrap"].write_id
    order         = pveguest_file.application_files_order.write_id
  }
}
