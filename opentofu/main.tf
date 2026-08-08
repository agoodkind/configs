locals {
  shared_vars = yamldecode(
    file("${path.module}/../ansible/inventory/group_vars/all/vars.yml")
  )
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
