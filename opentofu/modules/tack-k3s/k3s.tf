locals {
  node_ipv6 = split("/", var.guest.ipv6_cidr)[0]
  cluster_configuration = merge({
    "cluster-init"                = true
    "secrets-encryption"          = true
    "node-name"                   = var.guest.hostname
    "node-ip"                     = local.node_ipv6
    "advertise-address"           = local.node_ipv6
    "bind-address"                = "::"
    "tls-san"                     = [var.guest.hostname, local.node_ipv6]
    "cluster-cidr"                = var.cluster_network.pod_cidr
    "service-cidr"                = var.cluster_network.service_cidr
    "cluster-dns"                 = var.cluster_network.dns_address
    "flannel-ipv6-masq"           = true
    "disable"                     = ["traefik", "servicelb"]
    "token-file"                  = "/etc/rancher/k3s/server-token"
    "write-kubeconfig-mode"       = "0600"
    "etcd-snapshot-compress"      = true
    "etcd-snapshot-schedule-cron" = "0 */12 * * *"
    "etcd-snapshot-retention"     = 5
    "etcd-s3"                     = true
    "etcd-s3-endpoint"            = var.snapshot.endpoint
    "etcd-s3-insecure"            = var.snapshot.insecure
    "etcd-s3-bucket"              = var.snapshot.bucket
    "etcd-s3-folder"              = var.snapshot.folder
    "etcd-s3-retention"           = 5
    "etcd-s3-bucket-lookup-type"  = "path"
    }, var.snapshot.region == null ? {} : {
    "etcd-s3-region" = var.snapshot.region
    }, var.snapshot.ca_pem == null ? {} : {
    "etcd-s3-endpoint-ca" = "/etc/rancher/k3s/s3-ca.pem"
  })
}

resource "pveguest_download" "k3s" {
  node   = var.guest.node
  vmid   = proxmox_virtual_environment_vm.server.vm_id
  kind   = "qemu"
  url    = var.k3s_artifact.url
  sha256 = var.k3s_artifact.sha256
  fetch  = "controller"
  path   = "/usr/local/bin/k3s"
  mode   = "0755"

  depends_on = [module.base]
}

resource "pveguest_file" "configuration" {
  node    = var.guest.node
  vmid    = proxmox_virtual_environment_vm.server.vm_id
  kind    = "qemu"
  path    = "/etc/rancher/k3s/config.yaml"
  mode    = "0600"
  content = yamlencode(local.cluster_configuration)
}

resource "pveguest_file" "server_token" {
  node               = var.guest.node
  vmid               = proxmox_virtual_environment_vm.server.vm_id
  kind               = "qemu"
  path               = "/etc/rancher/k3s/server-token"
  mode               = "0600"
  content_wo         = "${trimspace(var.server_token)}\n"
  content_wo_version = var.server_token_version
}

resource "pveguest_file" "snapshot_credentials" {
  node = var.guest.node
  vmid = proxmox_virtual_environment_vm.server.vm_id
  kind = "qemu"
  path = "/etc/rancher/k3s/config.yaml.d/90-s3-credentials.yaml"
  mode = "0600"
  content_wo = yamlencode({
    "etcd-s3-access-key" = var.snapshot_credentials.access_key
    "etcd-s3-secret-key" = var.snapshot_credentials.secret_key
  })
  content_wo_version = var.snapshot_credentials_version
}

resource "pveguest_file" "snapshot_ca" {
  for_each = var.snapshot.ca_pem == null ? {} : { ca = var.snapshot.ca_pem }

  node    = var.guest.node
  vmid    = proxmox_virtual_environment_vm.server.vm_id
  kind    = "qemu"
  path    = "/etc/rancher/k3s/s3-ca.pem"
  mode    = "0644"
  content = each.value
}

resource "pveguest_file" "service" {
  node    = var.guest.node
  vmid    = proxmox_virtual_environment_vm.server.vm_id
  kind    = "qemu"
  path    = "/etc/systemd/system/k3s.service"
  content = file("${path.module}/files/k3s.service")
}

resource "pveguest_systemd_unit" "server" {
  node    = var.guest.node
  vmid    = proxmox_virtual_environment_vm.server.vm_id
  kind    = "qemu"
  name    = "k3s.service"
  enabled = true
  active  = true

  restart_on = merge({
    binary               = pveguest_download.k3s.write_id
    configuration        = pveguest_file.configuration.write_id
    server_token         = pveguest_file.server_token.write_id
    snapshot_credentials = pveguest_file.snapshot_credentials.write_id
    registry             = pveguest_file.registry.write_id
    application_files    = pveguest_file.application_files_order.write_id
    service              = pveguest_file.service.write_id
  }, { for key, certificate in pveguest_file.snapshot_ca : key => certificate.write_id })

  depends_on = [pveguest_systemd_unit.application_files]
}
