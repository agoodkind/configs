terraform {
  required_providers {
    linux = {
      source  = "labbs/linux"
      version = "1.3.0"
    }
  }
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
  archive     = "send-email_linux_amd64.tar.gz"
  install_dir = "/opt/send-email"
  binary      = "${local.install_dir}/send-email"
  sendmail    = "/usr/sbin/sendmail"
}

resource "linux_download" "send_email" {
  url    = "https://github.com/agoodkind/send-email/releases/download/${var.release_tag}/${local.archive}"
  sha256 = var.sha256
  path   = "/var/cache/send-email/${local.archive}"
}

resource "linux_archive_extract" "send_email" {
  source      = linux_download.send_email.path
  destination = local.install_dir

  triggers = {
    sha256 = linux_download.send_email.sha256
  }
}

resource "linux_symlink" "send_email" {
  path   = "/usr/local/bin/send-email"
  target = local.binary

  depends_on = [linux_archive_extract.send_email]
}

resource "linux_command" "divert_sendmail" {
  create = "dpkg-divert --add --rename --divert ${local.sendmail}.distrib ${local.sendmail}"
  read   = "dpkg-divert --truename ${local.sendmail}"
  delete = "rm -f ${local.sendmail} && dpkg-divert --rename --remove ${local.sendmail}"
}

resource "linux_symlink" "sendmail" {
  path   = local.sendmail
  target = local.binary

  depends_on = [linux_archive_extract.send_email, linux_command.divert_sendmail]
}

resource "linux_file" "mail_env" {
  path = "/etc/send-email/mail.env"
  content = templatefile("${path.module}/mail.env.tftpl", {
    local_recipient = var.local_recipient
  })
}
