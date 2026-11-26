module "configuration" { source = "../../modules/vault/terrakube-prod/configuration" }
module "contracts" { source = "../../modules/contracts" }

locals {
  guest     = module.configuration.guest
  shared    = module.contracts.shared_vars
  api_token = "${module.contracts.proxmox_token_principal}=${var.vault_proxmox_token_secret}"
}

data "http" "github_ssh_keys" {
  url = "https://github.com/${local.shared.github_ssh_keys_user}.keys"

  lifecycle {
    postcondition {
      condition = self.status_code == 200 && length([
        for line in split("\n", replace(self.response_body, "\r", "")) : line
        if trimspace(line) != ""
        ]) > 0 && alltrue([
        for line in split("\n", replace(self.response_body, "\r", "")) :
        can(regex("^(ssh-[A-Za-z0-9@._+-]+|ecdsa-[A-Za-z0-9@._+-]+|sk-[A-Za-z0-9@._+-]+) [A-Za-z0-9+/]+={0,3}( .*)?$", trimspace(line)))
        if trimspace(line) != ""
      ])
      error_message = "GitHub returned an invalid SSH public key response for ${local.shared.github_ssh_keys_user}."
    }
  }
}

module "github_keys" {
  source        = "../../modules/github-keys"
  response_body = data.http.github_ssh_keys.response_body
}

module "vm" {
  source      = "../../modules/platform/cloud-vm"
  guest       = local.guest
  cloud_image = module.configuration.cloud_image
  vm_capacity = { cores = 8, memory_mib = 16384, disk_gib = 120 }
  tags        = ["terrakube", "k3s"]
  pci_mapping = proxmox_hardware_mapping_pci.terrakube.name
  initialization = {
    public_keys     = module.github_keys.public_keys
    packages        = ["qemu-guest-agent", "ca-certificates", "curl", "ethtool", "iproute2", "iptables", "netplan.io"]
    filename_prefix = "terrakube"
  }
}

module "network" {
  source      = "../../modules/vault/guest-network"
  guest       = merge(module.configuration.network, { vmid = module.vm.vmid })
  phase       = "active"
  dns_servers = local.guest.dns_servers
}

module "base" {
  source = "../../modules/base"

  node = local.guest.node
  vmid = module.vm.vmid
  kind = "qemu"
  name = local.guest.service_key

  networkd_wait_online_enabled             = true
  public_keys                              = module.github_keys.public_keys
  restricted_key_lines                     = []
  timezone                                 = "America/Los_Angeles"
  login_dir                                = local.shared.login_dir
  revision                                 = local.shared.guest_prep_revision
  revision_file                            = local.shared.guest_prep_revision_file
  scripts_updater_activation_run           = false
  scripts_updater_randomized_delay_seconds = 300
}

module "k3s" {
  source = "../../modules/platform/k3s"

  guest                 = module.vm.guest
  k3s_artifact          = module.configuration.k3s_artifact
  cluster_network       = module.configuration.k3s_network
  snapshots_enabled     = false
  registry_auth_enabled = false
  server_token          = var.vault_terrakube_k3s_server_token
  server_token_version  = 1

  depends_on = [module.base, module.network]
}
