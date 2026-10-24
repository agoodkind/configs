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

module "vault" {
  source = "../../modules/vault/proxy"

  providers = { proxmox = proxmox }

  ssh_keys        = trimspace(data.http.github_ssh_keys.response_body)
  service_mapping = local.service_mapping
  host_networks   = module.contracts.host_networks
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

locals { proxmox_token_principal = module.contracts.proxmox_token_principal }
