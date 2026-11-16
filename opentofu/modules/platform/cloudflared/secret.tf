locals {
  token_secret_name = "${var.connector_prefix}-token-${var.tunnel_id}-v${var.token_version}"
}

resource "terraform_data" "tunnel_identity" {
  triggers_replace = var.tunnel_id
}

resource "pveguest_file" "token" {
  node = var.guest.node
  vmid = var.guest.vmid
  kind = "qemu"
  path = "/var/lib/rancher/k3s/server/manifests/${var.manifest_prefix}-token.yaml"
  mode = "0600"
  content_wo = yamlencode({
    apiVersion = "v1"
    kind       = "Secret"
    metadata   = { name = local.token_secret_name, namespace = var.namespace }
    type       = "Opaque"
    data       = { token = base64encode(var.token) }
  })
  content_wo_version = var.token_version

  lifecycle {
    replace_triggered_by = [terraform_data.tunnel_identity]
  }
}
