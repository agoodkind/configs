terraform {
  required_providers {
    system = {
      source  = "neuspaces/system"
      version = "0.5.0"
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

resource "system_link" "sendmail" {
  path   = "/usr/sbin/sendmail"
  target = "/usr/local/bin/send-email"

  depends_on = [terraform_data.install]
}

resource "system_folder" "send_email" {
  path = "/etc/send-email"
  mode = "755"
}

resource "system_file" "mail_env" {
  path = "/etc/send-email/mail.env"
  content = templatefile("${path.module}/mail.env.tftpl", {
    local_recipient = var.local_recipient
  })
  mode = "644"

  depends_on = [system_folder.send_email]
}
