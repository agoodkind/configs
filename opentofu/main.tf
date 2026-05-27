data "http" "github_ssh_keys" {
  url = "https://github.com/${var.github_ssh_keys_user}.keys"
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
