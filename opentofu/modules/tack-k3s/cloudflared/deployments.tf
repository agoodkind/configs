locals {
  image      = "docker.io/cloudflare/cloudflared@sha256:4955524aeaa379e154023d5a027c250f7b35041a2e7e950cf13f144bc9b9c5ba"
  connectors = { a = 2000, b = 2001 }
  pod_templates = {
    for connector, port in local.connectors : connector => {
      metadata = { labels = { app = "cloudflared-${connector}" } }
      spec = {
        hostNetwork                  = true
        dnsPolicy                    = "ClusterFirstWithHostNet"
        dnsConfig                    = { nameservers = var.guest.dns_servers }
        automountServiceAccountToken = false
        securityContext = {
          runAsUser    = 65532
          runAsGroup   = 65532
          fsGroup      = 65532
          runAsNonRoot = true
        }
        containers = [{
          name  = "cloudflared"
          image = local.image
          args = [
            "tunnel", "--no-autoupdate", "--edge-ip-version", "auto",
            "--protocol", "auto", "--metrics", "[::1]:${port}",
            "run", "--token-file", "/etc/cloudflared/token",
          ]
          securityContext = {
            allowPrivilegeEscalation = false
            readOnlyRootFilesystem   = true
            capabilities             = { drop = ["ALL"] }
          }
          startupProbe = {
            httpGet          = { host = "::1", path = "/ready", port = port }
            periodSeconds    = 5
            timeoutSeconds   = 2
            failureThreshold = 30
          }
          readinessProbe = {
            httpGet          = { host = "::1", path = "/ready", port = port }
            periodSeconds    = 5
            timeoutSeconds   = 2
            failureThreshold = 3
          }
          livenessProbe = {
            httpGet          = { host = "::1", path = "/ready", port = port }
            periodSeconds    = 10
            timeoutSeconds   = 2
            failureThreshold = 6
          }
          volumeMounts = [{ name = "token", mountPath = "/etc/cloudflared", readOnly = true }]
        }]
        volumes = [{
          name   = "token"
          secret = { secretName = local.token_secret_name, defaultMode = 288 }
        }]
      }
    }
  }
  revisions = {
    for connector, template in local.pod_templates : connector => substr(sha256(jsonencode({
      template      = template
      tunnel_id     = var.tunnel_id
      token_version = var.token_version
    })), 0, 40)
  }
  deployments = {
    for connector, template in local.pod_templates : connector => {
      apiVersion = "apps/v1"
      kind       = "Deployment"
      metadata   = { name = "cloudflared-${connector}", namespace = "tack" }
      spec = {
        replicas = 1
        selector = { matchLabels = template.metadata.labels }
        strategy = { type = "RollingUpdate", rollingUpdate = { maxSurge = 0, maxUnavailable = 1 } }
        template = merge(template, {
          metadata = { labels = merge(template.metadata.labels, { "tack-revision" = local.revisions[connector] }) }
        })
      }
    }
  }
}
