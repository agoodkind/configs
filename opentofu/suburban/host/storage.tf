resource "proxmox_storage_directory" "local" {
  provider = proxmox.suburban

  id      = "local"
  path    = "/var/lib/vz"
  nodes   = []
  content = ["iso", "rootdir", "images", "backup", "vztmpl", "snippets", "import"]
  shared  = false
  disable = false

  lifecycle {
    prevent_destroy = true
  }
}

import {
  to = proxmox_storage_directory.local
  id = "local"
}
