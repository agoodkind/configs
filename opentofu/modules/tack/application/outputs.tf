output "origin" {
  value = "http://${var.identity.name}.${var.identity.namespace}.svc.cluster.local:${var.identity.port}"
}
