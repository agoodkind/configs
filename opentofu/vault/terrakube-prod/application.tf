locals {
  namespace     = module.configuration.namespace
  manifest_path = "/var/lib/rancher/k3s/server/manifests"
  chart_version = "4.7.20"
  chart_file    = "terrakube-${local.chart_version}.tgz"
  dashboard_url = "https://${module.configuration.dashboard.hostname}"
  chart_values = templatefile("${path.module}/files/values.yaml.tftpl", {
    hostname        = module.configuration.dashboard.hostname
    issuer          = "${local.dashboard_url}/dex"
    storage         = var.terrakube_artifact_storage
    secret_revision = tostring(var.terrakube_credentials_version)
  })
  proxy_configuration = templatefile("${path.module}/files/nginx.conf.tftpl", {
    dashboard = module.configuration.dashboard
    webhook   = module.configuration.webhook
    dns       = module.configuration.k3s_network.dns_address
  })
  workloads = templatefile("${path.module}/files/workloads.yaml.tftpl", {
    namespace       = local.namespace
    dashboard_ipv6  = module.configuration.dashboard.ipv6
    proxy_config    = indent(4, local.proxy_configuration)
    proxy_revision  = sha256(local.proxy_configuration)
    secret_revision = tostring(var.terrakube_credentials_version)
  })
  chart_manifest = yamlencode({
    apiVersion = "helm.cattle.io/v1"
    kind       = "HelmChart"
    metadata   = { name = "terrakube", namespace = "kube-system" }
    spec = {
      chart           = "https://%%{KUBERNETES_API}%/static/charts/${local.chart_file}"
      targetNamespace = local.namespace
      version         = local.chart_version
      failurePolicy   = "abort"
      valuesContent   = local.chart_values
    }
  })
}

resource "pveguest_download" "chart" {
  node   = local.guest.node
  vmid   = module.vm.vmid
  kind   = "qemu"
  url    = "https://github.com/terrakube-io/terrakube-helm-chart/releases/download/terrakube-${local.chart_version}/${local.chart_file}"
  sha256 = "f042b74f634c499c5314859590e3963fd0cfa8c36433671d2435c1520cb239ab"
  fetch  = "guest"
  path   = "/var/lib/rancher/k3s/server/static/charts/${local.chart_file}"
  mode   = "0644"

  depends_on = [module.k3s]
}

resource "pveguest_file" "namespace" {
  node = local.guest.node
  vmid = module.vm.vmid
  kind = "qemu"
  path = "${local.manifest_path}/terrakube-namespace.yaml"
  content = join("---\n", [
    yamlencode({ apiVersion = "v1", kind = "Namespace", metadata = { name = local.namespace } }),
    yamlencode({
      apiVersion = "v1"
      kind       = "ConfigMap"
      metadata   = { name = "coredns-custom", namespace = "kube-system" }
      data = {
        "terrakube.server" = <<-CORE
          ${module.configuration.dashboard.hostname}:53 {
            hosts {
              ${module.configuration.dashboard.ipv6} ${module.configuration.dashboard.hostname}
            }
          }
        CORE
      }
    }),
  ])

  depends_on = [module.k3s]
}

resource "pveguest_file" "workloads" {
  node    = local.guest.node
  vmid    = module.vm.vmid
  kind    = "qemu"
  path    = "${local.manifest_path}/terrakube-workloads.yaml"
  content = local.workloads

  depends_on = [pveguest_file.secrets]
}

resource "pveguest_file" "chart" {
  node    = local.guest.node
  vmid    = module.vm.vmid
  kind    = "qemu"
  path    = "${local.manifest_path}/terrakube-chart.yaml"
  content = local.chart_manifest

  depends_on = [pveguest_download.chart, pveguest_file.workloads]
}
