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

variable "release_tag" {
  type = string
}

variable "sha256" {
  type = string
}

variable "local_recipient" {
  type = string
}

locals {
  install_script = templatefile("${path.module}/install.sh.tftpl", {
    release_tag = var.release_tag
    sha256      = var.sha256
  })
}

resource "terraform_data" "install" {
  triggers_replace = [sha256(local.install_script)]

  connection {
    type  = "ssh"
    host  = var.ssh_host
    user  = "root"
    agent = true
  }

  provisioner "file" {
    content     = local.install_script
    destination = "/tmp/install-send-email.sh"
  }

  provisioner "remote-exec" {
    inline = ["bash /tmp/install-send-email.sh"]
  }
}

resource "remote_file" "mail_env" {
  conn {
    host  = var.ssh_host
    user  = "root"
    agent = true
  }

  path = "/etc/send-email/mail.env"
  content = templatefile("${path.module}/mail.env.tftpl", {
    local_recipient = var.local_recipient
  })
  permissions = "0644"

  depends_on = [terraform_data.install]
}
