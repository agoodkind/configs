terraform {
  required_providers {
    remote = {
      source  = "tenstad/remote"
      version = ">= 0.2.1"
    }
  }
}

variable "ssh_host" {
  type = string
}

variable "mail_to" {
  type = string
}

resource "terraform_data" "install" {
  triggers_replace = [filesha256("${path.module}/install.sh")]

  connection {
    type  = "ssh"
    host  = var.ssh_host
    user  = "root"
    agent = true
  }

  provisioner "remote-exec" {
    script = "${path.module}/install.sh"
  }
}

resource "remote_file" "auto_upgrades" {
  conn {
    host  = var.ssh_host
    user  = "root"
    agent = true
  }

  path        = "/etc/apt/apt.conf.d/20auto-upgrades"
  content     = file("${path.module}/20auto-upgrades")
  permissions = "0644"

  depends_on = [terraform_data.install]
}

resource "remote_file" "local_settings" {
  conn {
    host  = var.ssh_host
    user  = "root"
    agent = true
  }

  path = "/etc/apt/apt.conf.d/52unattended-upgrades-local"
  content = templatefile("${path.module}/52unattended-upgrades-local.tftpl", {
    mail_to = var.mail_to
  })
  permissions = "0644"

  depends_on = [terraform_data.install]
}
