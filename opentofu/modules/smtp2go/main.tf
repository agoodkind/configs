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

variable "api_key" {
  type      = string
  sensitive = true
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

resource "remote_file" "smtp2go_env" {
  conn {
    host  = var.ssh_host
    user  = "root"
    agent = true
  }

  path = "/etc/send-email/smtp2go.env"
  content = templatefile("${path.module}/smtp2go.env.tftpl", {
    api_key = var.api_key
  })
  permissions = "0600"

  depends_on = [terraform_data.install]
}
