locals { repository_root = abspath("${path.module}/../../..") }

module "contracts" {
  source          = "../../modules/contracts"
  repository_root = local.repository_root
}

locals {
  shared_vars         = module.contracts.shared_vars
  service_mapping     = module.contracts.service_mapping
  cloudflare_zone_ids = module.contracts.cloudflare_zone_ids
}

module "guest_inputs" {
  source          = "../../modules/guest-inputs"
  guest_keys      = toset(["mwan_failover_suburban", "mwan_suburban"])
  shared_vars     = local.shared_vars
  service_mapping = local.service_mapping
  proxy_vars      = module.contracts.proxy_vars
  timezone        = "America/Los_Angeles"
}

module "github_keys" {
  source        = "../../modules/github-keys"
  response_body = data.http.github_ssh_keys.response_body
}

locals {
  guests                  = module.guest_inputs.guests
  guest_kinds             = module.guest_inputs.guest_kinds
  guest_kind_values       = module.guest_inputs.guest_kind_values
  mail_relays             = module.guest_inputs.mail_relays
  sshpiper_lines          = module.guest_inputs.sshpiper_lines
  github_keys             = module.github_keys.public_keys
  proxmox_token_principal = module.contracts.proxmox_token_principal
  nodes = {
    suburban = {
      endpoint  = "https://[${local.service_mapping.suburban_hypervisor.ipv6}]:8006"
      api_token = "${local.proxmox_token_principal}=${var.vault_suburban_testbed_pve_token_secret}"
      insecure  = true
      node_name = "hypervisor"
    }
  }
}

module "base" {
  source   = "../../modules/base"
  for_each = local.guests

  node = each.value.node
  vmid = each.value.vmid
  kind = each.value.kind
  name = each.key

  authorized_key_lines        = each.value.authorized_key_lines
  extra_authorized_keys_files = each.value.extra_authorized_keys_files
  public_keys                 = each.value.authorized_key_lines == null ? local.github_keys : []
  restricted_key_lines        = each.value.authorized_key_lines == null ? local.sshpiper_lines : []
  mail_relay                  = try(local.mail_relays[each.key], null)
  timezone                    = each.value.timezone
  login_dir                   = local.shared_vars.login_dir
  revision                    = local.shared_vars.guest_prep_revision
  revision_file               = local.shared_vars.guest_prep_revision_file

  scripts_updater_activation_run           = each.value.scripts_updater_timer.activation_run
  scripts_updater_randomized_delay_seconds = each.value.scripts_updater_timer.randomized_delay_seconds
}

# Ansible created the SMTP2GO user and /etc/msmtprc on each guest.
# The pveguest provider imports the existing mail configuration and keeps the
# SMTP2GO password. A new pveguest_mail_relay resource would rotate the
# password.
import {
  for_each = local.mail_relays
  to       = module.base[each.key].pveguest_mail_relay.smtp["smtp2go"]
  id       = "${local.guests[each.key].node}/${local.guests[each.key].vmid}/${local.guests[each.key].kind}/${each.value.username}/${each.value.password_version}"
}

module "suburban" {
  source = "../../modules/suburban/mwan"

  providers = { proxmox = proxmox.suburban }

  ssh_keys        = trimspace(data.http.github_ssh_keys.response_body)
  service_mapping = local.service_mapping
  host_networks   = module.contracts.host_networks
}

output "mwan_suburban_vmid" {
  description = "VMID assigned to the suburban MWAN VM"
  value       = module.suburban.mwan_suburban_vmid
}

resource "pveguest_file" "failover_dhcp6" {
  node = "suburban"
  vmid = local.service_mapping.mwan_failover_suburban.vmid
  kind = "lxc"

  path    = "/etc/systemd/system/mwan-failover-dhcp6.service"
  content = file("${local.repository_root}/mwan-failover/dhcp6.service")
  mode    = "0644"
}

resource "pveguest_systemd_unit" "failover_dhcp6" {
  node = "suburban"
  vmid = local.service_mapping.mwan_failover_suburban.vmid
  kind = "lxc"

  name       = "mwan-failover-dhcp6.service"
  enabled    = true
  active     = true
  restart_on = { unit = pveguest_file.failover_dhcp6.write_id }
}

data "http" "github_ssh_keys" {
  url = "https://github.com/${local.shared_vars.github_ssh_keys_user}.keys"

  lifecycle {
    postcondition {
      condition = self.status_code == 200 && length([
        for line in split("\n", replace(self.response_body, "\r", "")) : line
        if trimspace(line) != ""
        ]) > 0 && alltrue([
        for line in split("\n", replace(self.response_body, "\r", "")) :
        can(regex(
          "^(ssh-[A-Za-z0-9@._+-]+|ecdsa-[A-Za-z0-9@._+-]+|sk-[A-Za-z0-9@._+-]+) [A-Za-z0-9+/]+={0,3}( .*)?$",
          trimspace(line),
        ))
        if trimspace(line) != ""
      ])
      error_message = "GitHub returned an invalid SSH public key response for ${local.shared_vars.github_ssh_keys_user}."
    }
    precondition {
      condition = alltrue([
        for kind in values(local.guest_kinds) : contains(local.guest_kind_values, kind)
      ])
      error_message = "Enrolled guest kinds must be lxc or qemu."
    }
  }
}
