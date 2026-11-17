terraform {
  required_version = ">= 1.11"
  required_providers {
    proxmox  = { source = "bpg/proxmox", version = "0.116.0" }
    pveguest = { source = "tofu.home.arpa/agoodkind/pveguest", version = "0.1.4" }
  }
}

variable "vault_tofu_state_passphrase" {
  type      = string
  sensitive = true
}

variable "vault_poweredge_proxmox_root_password" {
  type      = string
  sensitive = true
  ephemeral = true
}

variable "vault_poweredge_pve_token_secret" {
  type      = string
  sensitive = true
  ephemeral = true
}

variable "poweredge_api_tunnel_port" {
  description = "Set this value to the local port of an SSH tunnel to port 8006 on PowerEdge. Verify the SSH host key before using the tunnel."
  type        = number
  default     = null
}

module "poweredge_hypervisor" {
  source = "../../modules/poweredge/services/poweredge_hypervisor"
}

locals {
  api_endpoint = var.poweredge_api_tunnel_port == null ? "https://${module.poweredge_hypervisor.value.hostname}:8006" : "https://127.0.0.1:${var.poweredge_api_tunnel_port}"
}

provider "proxmox" {
  endpoint = local.api_endpoint
  insecure = var.poweredge_api_tunnel_port != null
  username = "root@pam"
  password = var.vault_poweredge_proxmox_root_password
}

provider "pveguest" {
  nodes = {
    poweredge = {
      endpoint  = local.api_endpoint
      api_token = "${module.contracts.proxmox_token_principal}=${var.vault_poweredge_pve_token_secret}"
      insecure  = var.poweredge_api_tunnel_port != null
    }
  }
}

module "contracts" {
  source = "../../modules/contracts"
}

module "configuration" {
  source = "../../modules/poweredge/permissions/configuration"
}

data "proxmox_virtual_environment_user" "automation" {
  user_id = module.contracts.shared_vars.proxmox_api_user
}

locals {
  node_path   = "/nodes/poweredge"
  kernel_role = "ScopedKernelModules"

  # A user with another role on this node cannot inherit the kernel role from /.
  node_overrides_root = anytrue([
    for acl in data.proxmox_virtual_environment_user.automation.acl :
    acl.path == local.node_path && acl.role_id != local.kernel_role
  ])
}

resource "proxmox_acl" "kernel_modules" {
  count = local.node_overrides_root ? 1 : 0

  path      = local.node_path
  user_id   = data.proxmox_virtual_environment_user.automation.user_id
  role_id   = local.kernel_role
  propagate = true
}

data "pveguest_host_kernel_modules" "automation" {
  node       = "poweredge"
  depends_on = [proxmox_acl.kernel_modules]
}
