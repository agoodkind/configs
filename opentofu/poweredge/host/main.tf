locals {
  ssh_host               = "3d06:bad:b01:300::254"
  send_email_release_tag = "202611101607-1c-51b488f"
  send_email_sha256      = "7eb59cc727ba4c26c49c7d38a0fd2cabfaa47bcea72d942068cb7edf2124e85e"
}

module "unattended_upgrades" {
  source = "../../modules/unattended-upgrades"

  ssh_host = local.ssh_host
  mail_to  = "root"
}

module "mail" {
  source = "../../modules/mail"

  ssh_host        = local.ssh_host
  release_tag     = local.send_email_release_tag
  sha256          = local.send_email_sha256
  local_recipient = "alex@goodkind.io"

  depends_on = [terraform_data.hosts]
}

module "smtp2go" {
  source = "../../modules/smtp2go"

  ssh_host = local.ssh_host
  api_key  = var.vault_smtp2go_api_key
}

resource "terraform_data" "hosts" {
  triggers_replace = [filesha256("${path.module}/hosts.sh")]

  connection {
    type  = "ssh"
    host  = local.ssh_host
    user  = "root"
    agent = true
  }

  provisioner "remote-exec" {
    script = "${path.module}/hosts.sh"
  }
}
