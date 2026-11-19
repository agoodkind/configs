terraform {
  required_version = ">= 1.11"
  required_providers {
    pveguest = { source = "tofu.home.arpa/agoodkind/pveguest", version = ">= 0.1.3" }
  }
}

locals {
  application_labels = { app = var.identity.name }
  application_environment = merge(var.application.environment, {
    DATABASE_URL_FILE          = "/run/secrets/DATABASE_URL"
    AUDIT_OPERATOR_DSN_FILE    = "/run/secrets/AUDIT_OPERATOR_DSN"
    OPENSEARCH_PASSWORD_FILE   = "/run/secrets/OPENSEARCH_PASSWORD"
    OPENSEARCH_CURSOR_KEY_FILE = "/run/secrets/OPENSEARCH_CURSOR_KEY"
    OPENSEARCH_CA              = "/etc/tack/search-ca.crt"
    FDB_CLUSTER_FILE           = "/etc/foundationdb/fdb.cluster"
    LOG_JSON_FILE              = "/var/log/tack/app.jsonl"
    LOG_TEXT_FILE              = "/var/log/tack/app.log"
    LOG_COMMAND_DIR            = "/var/log/tack/commands"
  })
  application_deployment = {
    apiVersion = "apps/v1"
    kind       = "Deployment"
    metadata   = { name = var.identity.name, namespace = var.identity.namespace }
    spec = {
      replicas = var.application_replicas
      selector = { matchLabels = local.application_labels }
      strategy = merge(
        { type = var.application_strategy },
        var.application_strategy == "Recreate" ? {} : { rollingUpdate = { maxUnavailable = 0, maxSurge = 1 } },
      )
      template = {
        metadata = {
          labels = local.application_labels
          annotations = {
            "tack.goodkind.io/secret-versions" = jsonencode(var.application_secret_versions)
            "tack.goodkind.io/ledger-ca"       = sha256(var.application.ledger_ca_pem)
          }
        }
        spec = {
          terminationGracePeriodSeconds = 45
          automountServiceAccountToken  = false
          hostAliases = [for hostname, address in var.application.ledger_hosts : {
            ip = address, hostnames = [hostname]
          }]
          containers = [{
            name  = "app"
            image = var.application.image
            args  = ["serve", "--execute", "--operator-service", "tack-app"]
            ports = [{ name = "http", containerPort = var.identity.port }]
            env = concat([
              { name = "POD_NAME", valueFrom = { fieldRef = { fieldPath = "metadata.name" } } }
              ], [for name, value in local.application_environment : {
                name = name, value = value
            }])
            resources = {
              requests = { cpu = "250m", memory = "256Mi" }
              limits   = { cpu = "1", memory = "1Gi" }
            }
            readinessProbe = {
              httpGet          = { path = "/healthz", port = "http" }
              timeoutSeconds   = 3
              periodSeconds    = 10
              failureThreshold = 3
            }
            startupProbe = {
              tcpSocket        = { port = "http" }
              timeoutSeconds   = 3
              periodSeconds    = 5
              failureThreshold = 60
            }
            livenessProbe = {
              tcpSocket        = { port = "http" }
              timeoutSeconds   = 3
              periodSeconds    = 10
              failureThreshold = 3
            }
            volumeMounts = [
              { name = "credentials", mountPath = "/run/secrets", readOnly = true },
              { name = "certificates", mountPath = "/etc/tack", readOnly = true },
              { name = "foundationdb", mountPath = "/etc/foundationdb" },
              { name = "logs", mountPath = "/var/log/tack", subPathExpr = "$(POD_NAME)" },
            ]
          }]
          volumes = [
            { name = "credentials", hostPath = { path = "/var/lib/tack/secrets", type = "Directory" } },
            { name = "certificates", hostPath = { path = "/etc/tack/runtime", type = "Directory" } },
            { name = "foundationdb", hostPath = { path = "/etc/foundationdb", type = "Directory" } },
            { name = "logs", hostPath = { path = "/var/log/tack/app", type = "Directory" } },
          ]
        }
      }
    }
  }
  application_service = {
    apiVersion = "v1"
    kind       = "Service"
    metadata   = { name = var.identity.name, namespace = var.identity.namespace }
    spec = {
      type           = "ClusterIP"
      ipFamilyPolicy = "SingleStack"
      ipFamilies     = ["IPv6"]
      selector       = local.application_labels
      ports          = [{ name = "http", port = var.identity.port, targetPort = var.identity.port }]
    }
  }
}

resource "pveguest_file" "application_manifest" {
  node = var.guest.node
  vmid = var.guest.vmid
  kind = "qemu"
  path = "/var/lib/rancher/k3s/server/manifests/tack-app.yaml"
  content = join("---\n", [
    yamlencode({ apiVersion = "v1", kind = "Namespace", metadata = { name = var.identity.namespace } }),
    yamlencode(local.application_deployment),
    yamlencode(local.application_service),
  ])

  depends_on = [
    pveguest_systemd_unit.application_files,
    pveguest_file.application_secret,
    pveguest_file.ledger_ca,
  ]
}
