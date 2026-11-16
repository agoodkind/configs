locals {
  host                   = local.service_mapping.poweredge_hypervisor
  send_email_release_tag = "202611160557-1d-9875b96"
  send_email_sha256      = "ed240a4dd5baae5cb493861d294a4eed18e973d86615c00d3c279a15440203b4"
}

provider "linux" {
  ssh = {
    host     = local.host.ipv6
    host_key = local.host.ssh_host_key
  }
}

resource "linux_file_line" "hosts" {
  path              = "/etc/hosts"
  line              = "${local.host.ipv4} ${local.host.hostname} ${split(".", local.host.hostname)[0]}"
  match             = "^${replace(local.host.ipv4, ".", "\\.")}[[:space:]]"
  remove_on_destroy = false
}

module "unattended_upgrades" {
  source = "../../modules/unattended-upgrades"

  mail_to = "root"
}

module "mail" {
  source = "../../modules/mail"

  release_tag     = local.send_email_release_tag
  sha256          = local.send_email_sha256
  local_recipient = "alex@goodkind.io"

  depends_on = [linux_file_line.hosts]
}

module "smtp2go" {
  source = "../../modules/smtp2go"

  api_key = var.vault_smtp2go_api_key
}
