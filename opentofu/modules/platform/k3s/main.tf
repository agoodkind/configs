terraform {
  required_version = ">= 1.11"
  required_providers {
    pveguest = { source = "tofu.home.arpa/agoodkind/pveguest", version = ">= 0.1.3" }
  }
}

locals {
  node_ipv6 = split("/", var.guest.ipv6_cidr)[0]
  snapshot_configuration = {
    "etcd-snapshot-compress"      = true
    "etcd-snapshot-schedule-cron" = "0 */12 * * *"
    "etcd-snapshot-retention"     = 5
    "etcd-s3"                     = true
    "etcd-s3-endpoint"            = try(var.snapshot.endpoint, null)
    "etcd-s3-insecure"            = try(var.snapshot.insecure, null)
    "etcd-s3-bucket"              = try(var.snapshot.bucket, null)
    "etcd-s3-folder"              = try(var.snapshot.folder, null)
    "etcd-s3-retention"           = 5
    "etcd-s3-bucket-lookup-type"  = "path"
    "etcd-s3-region"              = try(var.snapshot.region, null)
    "etcd-s3-endpoint-ca"         = try(var.snapshot.ca_pem, null) == null ? null : "/etc/rancher/k3s/s3-ca.pem"
  }
  cluster_configuration = merge({
    "cluster-init"          = true
    "secrets-encryption"    = true
    "node-name"             = var.guest.hostname
    "node-ip"               = local.node_ipv6
    "advertise-address"     = local.node_ipv6
    "bind-address"          = "::"
    "tls-san"               = [var.guest.hostname, local.node_ipv6]
    "cluster-cidr"          = var.cluster_network.pod_cidr
    "service-cidr"          = var.cluster_network.service_cidr
    "cluster-dns"           = var.cluster_network.dns_address
    "flannel-ipv6-masq"     = true
    "disable"               = ["traefik", "servicelb"]
    "token-file"            = "/etc/rancher/k3s/server-token"
    "write-kubeconfig-mode" = "0600"
    }, {
    for key, value in local.snapshot_configuration : key => value
    if var.snapshots_enabled && value != null
    }, {
    for key, value in { "etcd-disable-snapshots" = true } : key => value
    if !var.snapshots_enabled
  })
}

module "runtime" {
  source                       = "../k3s-runtime"
  guest                        = var.guest
  k3s_artifact                 = var.k3s_artifact
  registry_credentials         = var.registry_credentials
  registry_credentials_version = var.registry_credentials_version
}

moved {
  from = pveguest_download.k3s
  to   = module.runtime.pveguest_download.k3s
}

moved {
  from = pveguest_file.registry
  to   = module.runtime.pveguest_file.registry
}

resource "pveguest_file" "configuration" {
  node    = var.guest.node
  vmid    = var.guest.vmid
  kind    = "qemu"
  path    = "/etc/rancher/k3s/config.yaml"
  mode    = "0600"
  content = yamlencode(local.cluster_configuration)
}

resource "pveguest_file" "server_token" {
  node               = var.guest.node
  vmid               = var.guest.vmid
  kind               = "qemu"
  path               = "/etc/rancher/k3s/server-token"
  mode               = "0600"
  content_wo         = "${trimspace(var.server_token)}\n"
  content_wo_version = var.server_token_version
}

resource "pveguest_file" "snapshot_credentials" {
  count = var.snapshots_enabled ? 1 : 0
  node  = var.guest.node
  vmid  = var.guest.vmid
  kind  = "qemu"
  path  = "/etc/rancher/k3s/config.yaml.d/90-s3-credentials.yaml"
  mode  = "0600"
  content_wo = yamlencode({
    "etcd-s3-access-key" = var.snapshot_credentials.access_key
    "etcd-s3-secret-key" = var.snapshot_credentials.secret_key
  })
  content_wo_version = var.snapshot_credentials_version
}

resource "pveguest_file" "snapshot_ca" {
  for_each = !var.snapshots_enabled || try(var.snapshot.ca_pem, null) == null ? {} : { ca = var.snapshot.ca_pem }

  node    = var.guest.node
  vmid    = var.guest.vmid
  kind    = "qemu"
  path    = "/etc/rancher/k3s/s3-ca.pem"
  mode    = "0644"
  content = each.value
}

resource "pveguest_file" "service" {
  node    = var.guest.node
  vmid    = var.guest.vmid
  kind    = "qemu"
  path    = "/etc/systemd/system/k3s.service"
  content = file("${path.module}/files/k3s.service")
}

resource "pveguest_systemd_unit" "server" {
  node    = var.guest.node
  vmid    = var.guest.vmid
  kind    = "qemu"
  name    = "k3s.service"
  enabled = true
  active  = true

  restart_on = merge({
    binary               = module.runtime.binary_write_id
    configuration        = pveguest_file.configuration.write_id
    server_token         = pveguest_file.server_token.write_id
    snapshot_credentials = try(pveguest_file.snapshot_credentials[0].write_id, "disabled")
    registry             = module.runtime.registry_write_id
    service              = pveguest_file.service.write_id
  }, { for key, certificate in pveguest_file.snapshot_ca : key => certificate.write_id })
}

moved {
  from = pveguest_file.snapshot_credentials
  to   = pveguest_file.snapshot_credentials[0]
}
