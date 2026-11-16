terraform {
  required_providers {
    system = {
      source  = "neuspaces/system"
      version = "0.5.0"
    }
  }
}

variable "mail_to" {
  type = string
}

resource "system_packages_apt" "unattended_upgrades" {
  package {
    name = "unattended-upgrades"
  }
}

resource "system_file" "auto_upgrades" {
  path    = "/etc/apt/apt.conf.d/20auto-upgrades"
  content = file("${path.module}/20auto-upgrades")
  mode    = "644"

  depends_on = [system_packages_apt.unattended_upgrades]
}

resource "system_file" "local_settings" {
  path = "/etc/apt/apt.conf.d/52unattended-upgrades-local"
  content = templatefile("${path.module}/52unattended-upgrades-local.tftpl", {
    mail_to = var.mail_to
  })
  mode = "644"

  depends_on = [system_packages_apt.unattended_upgrades]
}
