locals { repository_root = abspath("${path.module}/../../../..") }

module "suburban_config" {
  source = "../../../modules/suburban"
}

module "contracts" {
  source = "../../../modules/contracts"
}

locals {
  shared_vars     = module.contracts.shared_vars
  service_mapping = module.configuration.service_mapping
}

module "guest_inputs" {
  source           = "../../../modules/guest-inputs"
  guest_keys       = module.configuration.guest_keys
  shared_vars      = local.shared_vars
  service_mapping  = { for name in module.configuration.guest_keys : name => local.service_mapping[name] }
  proxy_public_key = module.configuration.proxy_public_key
  timezone         = "America/Los_Angeles"
  proxy_address    = module.configuration.proxy_address
}

module "github_keys" {
  source        = "../../../modules/github-keys"
  response_body = data.http.github_ssh_keys.response_body
}

locals {
  guests                  = module.guest_inputs.guests
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
  source   = "../../../modules/base"
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
  login_dir                   = local.shared_vars.login_dir
  revision                    = local.shared_vars.guest_prep_revision
  revision_file               = local.shared_vars.guest_prep_revision_file

  scripts_updater_activation_run           = each.value.scripts_updater_timer.activation_run
  scripts_updater_randomized_delay_seconds = each.value.scripts_updater_timer.randomized_delay_seconds

  depends_on = [module.suburban]
}

module "suburban" {
  source = "../../../modules/suburban/simulators/routing"

  providers = { proxmox = proxmox.suburban, pveguest = pveguest }

  ssh_keys           = trimspace(data.http.github_ssh_keys.response_body)
  service_mapping    = local.service_mapping
  host_networks      = merge(module.suburban_config.bridges, { for name, network in module.routing.networks : name => network.bridge })
  routing_networks   = module.routing.networks
  routing_management = module.routing.management
  guest_keys         = module.configuration.guest_keys
  frr                = module.configuration.frr
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
  }
}

module "configuration" { source = "../../../modules/suburban/simulators/tunnel-upstream/configuration" }

module "routing" { source = "../../../modules/suburban/routing" }
