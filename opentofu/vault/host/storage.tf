resource "proxmox_storage_directory" "local" {
  id      = "local"
  path    = "/var/lib/vz"
  nodes   = []
  content = ["snippets", "iso", "backup", "vztmpl", "images", "rootdir", "import"]
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
