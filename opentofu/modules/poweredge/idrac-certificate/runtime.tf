variable "workspace_directory" { type = string }
variable "factory_ca_file" { type = string }
variable "initial_installation" {
  type    = bool
  default = false
}
variable "credential_version" {
  type    = number
  default = 1
}

locals {
  runtime_root        = "/opt/configs/idrac-certificate"
  state_root          = "/var/lib/configs-idrac-certificate"
  operator_state      = pathexpand("~/.codex/state/idrac-certificate-runtime")
  configsctl_revision = "d4572f95665c509f036622003c4706b018ba3e09"
  configsctl_sha256   = "309eed4ca1b05a69b7e1008bcaeb92ffe9c7f8fd626a200edb2c6b44f81f042e"
  tofu_sha256         = "8f95cbe1523ef7b7913773634a6d6ac94c38f3c8eadba2e18b1e5b01567561ad"
  runtime_files = merge({
    "configsctl.yml" = {
      path    = "${local.runtime_root}/configsctl.yml"
      content = file("${path.module}/configsctl.yml")
    }
    "ansible.cfg" = {
      path    = "${local.runtime_root}/ansible/ansible.cfg"
      content = file("${path.module}/ansible.cfg")
    }
    "inventory" = {
      path    = "${local.runtime_root}/ansible/inventory/hosts.yml"
      content = file("${path.module}/inventory.yml")
    }
    "hosts" = {
      path    = "${local.runtime_root}/hosts"
      content = file("${path.module}/hosts")
    }
    "factory-ca" = {
      path    = "${local.runtime_root}/workspace/idrac-ca.pem"
      content = file(var.factory_ca_file)
    }
    "provider-lock" = {
      path    = "${local.runtime_root}/workspace/.terraform.lock.hcl"
      content = file("${var.workspace_directory}/.terraform.lock.hcl")
    }
    "service" = {
      path    = "/etc/systemd/system/idrac-certificate.service"
      content = file("${path.module}/idrac-certificate.service")
    }
    "timer" = {
      path    = "/etc/systemd/system/idrac-certificate.timer"
      content = file("${path.module}/idrac-certificate.timer")
    }
    "usb-interface" = {
      path    = "/etc/network/interfaces.d/idrac"
      content = file("${path.module}/usb-interface")
    }
    "checksums" = {
      path    = "${local.runtime_root}/binaries.sha256"
      content = "${local.configsctl_sha256}  ${local.runtime_root}/bin/configsctl\n${local.tofu_sha256}  ${local.runtime_root}/bin/tofu\n"
    }
    }, {
    for name in fileset(var.workspace_directory, "*.tf") : "workspace/${name}" => {
      path    = "${local.runtime_root}/workspace/${name}"
      content = file("${var.workspace_directory}/${name}")
    }
  })
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

resource "remote_file" "configuration" {
  for_each = local.runtime_files
  conn {
    host  = "[${var.ssh_host}]"
    user  = "root"
    agent = true
  }
  path        = each.value.path
  content     = each.value.content
  permissions = "0644"
  depends_on  = [terraform_data.directories]
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
    configuration      = sha256(jsonencode(local.runtime_files))
    configsctl         = local.configsctl_revision
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
      "/usr/bin/apt-get -o Acquire::Retries=0 -o Acquire::http::Timeout=10 -o Acquire::https::Timeout=10 update",
      "/usr/bin/apt-get install -y --no-install-recommends ansible-core",
      "/usr/bin/systemd-run --unit=idrac-certificate-stage-${self.id} --collect --property=Type=exec --property=RuntimeDirectory=idrac-certificate-stage-${self.id} --property=RuntimeDirectoryMode=0700 --property=RuntimeMaxSec=600 /usr/bin/sleep infinity",
    ]
  }
  provisioner "file" {
    source      = "${local.operator_state}/artifacts/${local.configsctl_revision}/linux-amd64/configsctl"
    destination = "${local.runtime_root}/bin/configsctl"
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
      "/usr/bin/chmod 0755 ${local.runtime_root}/bin/configsctl ${local.runtime_root}/bin/tofu",
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
    remote_file.configuration,
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
