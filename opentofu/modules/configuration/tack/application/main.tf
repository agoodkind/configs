locals {
  identity = {
    namespace                 = "tack"
    name                      = "tack-app"
    port                      = 8000
    connector_prefix          = "cloudflared"
    connector_manifest_prefix = "tack-cloudflared"
    revision_label            = "tack-revision"
  }
}

output "identity" {
  value = merge(local.identity, {
    origin = "http://${local.identity.name}.${local.identity.namespace}.svc.cluster.local.:${local.identity.port}"
  })
}

output "vm_recipe" {
  value = {
    tags              = ["tack", "k3s"]
    packages          = ["qemu-guest-agent", "curl", "iptables", "conntrack"]
    filename_prefix   = "tack-k3s"
    timezone          = "America/Los_Angeles"
    registry_username = "agoodkind"
  }
}
