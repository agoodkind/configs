resource "pveguest_file" "registry" {
  node = var.guest.node
  vmid = var.guest.vmid
  kind = "qemu"
  path = "/etc/rancher/k3s/registries.yaml"
  mode = "0600"
  content_wo = yamlencode({
    configs = {
      "ghcr.io" = {
        auth = {
          username = var.registry_credentials.username
          password = var.registry_credentials.token
        }
      }
    }
  })
  content_wo_version = var.registry_credentials_version
}
