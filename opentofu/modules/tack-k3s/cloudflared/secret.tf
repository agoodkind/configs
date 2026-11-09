resource "terraform_data" "tunnel_identity" {
  triggers_replace = var.tunnel_id
}

resource "pveguest_file" "token" {
  node = var.guest.node
  vmid = var.guest.vmid
  kind = "qemu"
  path = "/var/lib/rancher/k3s/server/manifests/tack-cloudflared-token.yaml"
  mode = "0600"
  content_wo = yamlencode({
    apiVersion = "v1"
    kind       = "Secret"
    metadata   = { name = "cloudflared-token", namespace = "tack" }
    type       = "Opaque"
    data       = { token = base64encode(var.token) }
  })
  content_wo_version = var.token_version

  lifecycle {
    replace_triggered_by = [terraform_data.tunnel_identity]
  }
}
