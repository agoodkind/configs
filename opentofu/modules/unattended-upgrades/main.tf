terraform {
  required_providers {
    linux = {
      source  = "labbs/linux"
      version = "1.3.0"
    }
  }
}

variable "mail_to" {
  type = string
}

resource "linux_package" "unattended_upgrades" {
  name = "unattended-upgrades"
}

resource "linux_file" "auto_upgrades" {
  path    = "/etc/apt/apt.conf.d/20auto-upgrades"
  content = file("${path.module}/20auto-upgrades")

  depends_on = [linux_package.unattended_upgrades]
}

resource "linux_file" "local_settings" {
  path = "/etc/apt/apt.conf.d/52unattended-upgrades-local"
  content = templatefile("${path.module}/52unattended-upgrades-local.tftpl", {
    mail_to = var.mail_to
  })

  depends_on = [linux_package.unattended_upgrades]
}
