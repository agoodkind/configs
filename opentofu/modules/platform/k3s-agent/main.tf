terraform {
  required_version = ">= 1.11"
  required_providers {
    pveguest = { source = "tofu.home.arpa/agoodkind/pveguest", version = ">= 0.1.3" }
  }
}

module "runtime" {
  source                       = "../k3s-runtime"
  guest                        = var.guest
  k3s_artifact                 = var.artifact
  registry_credentials         = var.registry_credentials
  registry_credentials_version = var.registry_credentials_version
}

resource "pveguest_file" "configuration" {
  node = var.guest.node
  vmid = var.guest.vmid
  kind = "qemu"
  path = "/etc/rancher/k3s/config.yaml"
  mode = "0600"
  content = yamlencode({
    server     = "https://[${var.server_ipv6}]:6443"
    token-file = "/etc/rancher/k3s/agent-token"
    node-name  = var.guest.hostname
    node-taint = var.taints
  })
}

resource "pveguest_file" "join_token" {
  node               = var.guest.node
  vmid               = var.guest.vmid
  kind               = "qemu"
  path               = "/etc/rancher/k3s/agent-token"
  mode               = "0600"
  content_wo         = "${trimspace(var.join_token)}\n"
  content_wo_version = var.join_token_version
}

resource "pveguest_file" "start" {
  node    = var.guest.node
  vmid    = var.guest.vmid
  kind    = "qemu"
  path    = "/usr/local/sbin/start-k3s-agent"
  mode    = "0755"
  content = file("${path.module}/files/start-k3s-agent.sh")
}

resource "pveguest_file" "service" {
  node    = var.guest.node
  vmid    = var.guest.vmid
  kind    = "qemu"
  path    = "/etc/systemd/system/k3s-agent.service"
  content = templatefile("${path.module}/files/k3s-agent.service.tftpl", { server_ipv6 = var.server_ipv6 })
}

resource "pveguest_systemd_unit" "agent" {
  node    = var.guest.node
  vmid    = var.guest.vmid
  kind    = "qemu"
  name    = "k3s-agent.service"
  enabled = true
  active  = true
  restart_on = {
    binary        = module.runtime.binary_write_id
    registry      = module.runtime.registry_write_id
    configuration = pveguest_file.configuration.write_id
    token         = pveguest_file.join_token.write_id
    start         = pveguest_file.start.write_id
    service       = pveguest_file.service.write_id
  }
}
