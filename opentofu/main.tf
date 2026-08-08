locals {
  shared_vars = yamldecode(
    file("${path.module}/../ansible/inventory/group_vars/all/vars.yml")
  )
}

data "http" "github_ssh_keys" {
  url = "https://github.com/${local.shared_vars.github_ssh_keys_user}.keys"

  lifecycle {
    postcondition {
      condition     = trimspace(self.response_body) != ""
      error_message = "GitHub returned no SSH public keys for the configured user."
    }
  }
}

module "suburban" {
  source = "./suburban"

  providers = {
    proxmox = proxmox.suburban
  }

  ssh_keys = trimspace(data.http.github_ssh_keys.response_body)
}

module "vault" {
  source = "./vault"

  providers = {
    proxmox = proxmox
  }

  ssh_keys = trimspace(data.http.github_ssh_keys.response_body)
}
