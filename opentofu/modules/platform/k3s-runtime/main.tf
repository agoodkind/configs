terraform {
  required_version = ">= 1.11"
  required_providers {
    pveguest = { source = "tofu.home.arpa/agoodkind/pveguest", version = ">= 0.1.3" }
  }
}

resource "pveguest_download" "k3s" {
  node   = var.guest.node
  vmid   = var.guest.vmid
  kind   = "qemu"
  url    = var.k3s_artifact.url
  sha256 = var.k3s_artifact.sha256
  fetch  = "guest"
  path   = "/usr/local/bin/k3s"
  mode   = "0755"
}

output "binary_write_id" { value = pveguest_download.k3s.write_id }
output "registry_write_id" { value = pveguest_file.registry.write_id }
