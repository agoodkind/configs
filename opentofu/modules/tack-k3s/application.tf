locals {
  application_labels = { app = "tack-app" }
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
    metadata   = { name = "tack-app", namespace = "tack" }
    spec = {
      replicas = 2
      selector = { matchLabels = local.application_labels }
      strategy = {
        type          = "RollingUpdate"
        rollingUpdate = { maxUnavailable = 0, maxSurge = 1 }
      }
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
            ports = [{ name = "http", containerPort = 8000 }]
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
    metadata   = { name = "tack-app", namespace = "tack" }
    spec = {
      type           = "ClusterIP"
      ipFamilyPolicy = "SingleStack"
      ipFamilies     = ["IPv6"]
      selector       = local.application_labels
      ports          = [{ name = "http", port = 8000, targetPort = 8000 }]
    }
  }
}

resource "pveguest_file" "application_manifest" {
  node = var.guest.node
  vmid = proxmox_virtual_environment_vm.server.vm_id
  kind = "qemu"
  path = "/var/lib/rancher/k3s/server/manifests/tack-app.yaml"
  content = join("---\n", [
    yamlencode({ apiVersion = "v1", kind = "Namespace", metadata = { name = "tack" } }),
    yamlencode(local.application_deployment),
    yamlencode(local.application_service),
  ])

  depends_on = [
    pveguest_systemd_unit.server,
    pveguest_file.application_secret,
    pveguest_file.ledger_ca,
  ]
}
