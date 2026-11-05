variable "artifact_sha256" { type = string }
variable "initial_installation" {
  type    = bool
  default = false
}
variable "credential_version" {
  type    = number
  default = 1
}

module "configuration" { source = "./configuration" }

locals {
  runtime_root   = module.configuration.runtime_root
  state_root     = module.configuration.state_root
  operator_state = pathexpand("~/.codex/state/idrac-certificate-runtime")
  tofu_sha256    = module.configuration.tofu_sha256
}

resource "terraform_data" "directories" {
  connection {
    type     = "ssh"
    host     = var.ssh_host
    user     = "root"
    agent    = true
    host_key = trimspace(file("${path.module}/poweredge-host-key.pub"))
  }
  provisioner "remote-exec" {
    inline = [
      "/usr/bin/install -d -m 0755 ${local.runtime_root}/bin ${local.runtime_root}/workspace ${local.runtime_root}/ansible/inventory/group_vars/all /etc/systemd/system/idrac-certificate.service.d",
      "/usr/bin/install -d -m 0700 ${local.state_root} /etc/credstore.encrypted",
    ]
  }
}

removed {
  from = remote_file.configuration

  lifecycle {
    destroy = false
  }
}

resource "remote_file" "initial_connection" {
  count = var.initial_installation ? 1 : 0
  conn {
    host  = "[${var.ssh_host}]"
    user  = "root"
    agent = true
  }
  path        = "/etc/systemd/system/idrac-certificate.service.d/initial-connection.conf"
  content     = "[Service]\nEnvironment=TF_VAR_idrac_endpoint=https://idrac-J9P5L44\nEnvironment=TF_VAR_idrac_initial_ca_file=${local.runtime_root}/workspace/idrac-ca.pem\n"
  permissions = "0644"
  depends_on  = [terraform_data.directories]
}

resource "terraform_data" "runtime" {
  triggers_replace = {
    artifact           = var.artifact_sha256
    tofu               = local.tofu_sha256
    credential_version = var.credential_version
  }
  connection {
    type     = "ssh"
    host     = var.ssh_host
    user     = "root"
    agent    = true
    host_key = trimspace(file("${path.module}/poweredge-host-key.pub"))
  }
  provisioner "remote-exec" {
    inline = [
      "/usr/bin/systemd-run --unit=idrac-certificate-stage-${self.id} --collect --property=Type=exec --property=RuntimeDirectory=idrac-certificate-stage-${self.id} --property=RuntimeDirectoryMode=0700 --property=RuntimeMaxSec=600 /usr/bin/sleep infinity",
    ]
  }
  provisioner "file" {
    source      = "${local.operator_state}/artifacts/opentofu/linux-amd64/tofu"
    destination = "${local.runtime_root}/bin/tofu"
  }
  provisioner "file" {
    source      = "${local.operator_state}/credentials/vault.yml"
    destination = "${local.runtime_root}/ansible/inventory/group_vars/all/vault.yml"
  }
  provisioner "file" {
    source      = "${local.operator_state}/credentials/vault.pass"
    destination = "/run/idrac-certificate-stage-${self.id}/vault.pass"
  }
  provisioner "remote-exec" {
    inline = [
      "/usr/bin/chmod 0755 ${local.runtime_root}/bin/tofu",
      "/usr/bin/chmod 0600 ${local.runtime_root}/ansible/inventory/group_vars/all/vault.yml",
      "/usr/bin/sha256sum --check ${local.runtime_root}/binaries.sha256",
      "/usr/bin/systemd-creds encrypt --with-key=host --name=vault.pass /run/idrac-certificate-stage-${self.id}/vault.pass /run/idrac-certificate-stage-${self.id}/vault.encrypted",
      "/usr/bin/install -m 0600 /run/idrac-certificate-stage-${self.id}/vault.encrypted /etc/credstore.encrypted/idrac-certificate-vault.pass",
      "/usr/bin/systemctl stop idrac-certificate-stage-${self.id}.service",
      "/usr/sbin/ifup idrac",
      "/usr/bin/systemctl daemon-reload",
      "/usr/bin/systemd-analyze verify /etc/systemd/system/idrac-certificate.service /etc/systemd/system/idrac-certificate.timer",
    ]
  }
  depends_on = [
    restful_operation.usb,
    remote_file.initial_connection,
  ]
}

data "tls_certificate" "ready" {
  url          = "https://idrac.poweredge.management.collingwood.goodkind.io"
  verify_chain = true
  depends_on   = [terraform_data.runtime]
}

resource "terraform_data" "activate" {
  triggers_replace = { runtime = terraform_data.runtime.id }
  connection {
    type     = "ssh"
    host     = var.ssh_host
    user     = "root"
    agent    = true
    host_key = trimspace(file("${path.module}/poweredge-host-key.pub"))
  }
  lifecycle {
    precondition {
      condition     = !var.initial_installation
      error_message = "Complete the first certificate installation and remove the temporary connection override before enabling daily renewal."
    }
  }
  provisioner "remote-exec" {
    inline = [
      "/usr/bin/systemctl daemon-reload",
      "/usr/bin/systemctl enable --now idrac-certificate.timer",
    ]
  }
  depends_on = [data.tls_certificate.ready, remote_file.initial_connection]
}
