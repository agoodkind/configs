locals {
  send_email_release_tag = "202611101607-1c-51b488f"
  send_email_sha256      = "7eb59cc727ba4c26c49c7d38a0fd2cabfaa47bcea72d942068cb7edf2124e85e"
}

module "unattended_upgrades" {
  source = "../../modules/unattended-upgrades"

  ssh_host = local.overlay_hosts.vault.ssh_host
  mail_to  = "root"
}

module "mail" {
  source = "../../modules/mail"

  ssh_host        = local.overlay_hosts.vault.ssh_host
  release_tag     = local.send_email_release_tag
  sha256          = local.send_email_sha256
  local_recipient = "alex@goodkind.io"
}

module "smtp2go" {
  source = "../../modules/smtp2go"

  ssh_host = local.overlay_hosts.vault.ssh_host
  api_key  = var.vault_smtp2go_api_key
}
