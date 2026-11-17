terraform {
  required_version = ">= 1.11"
  required_providers {
    proxmox = { source = "bpg/proxmox", version = "0.116.0" }
  }
}

variable "vault_tofu_state_passphrase" {
  type      = string
  sensitive = true
}

variable "poweredge_proxmox_root_password" {
  type      = string
  sensitive = true
  ephemeral = true
}

variable "poweredge_api_tunnel_port" {
  description = "Local port of an SSH tunnel to port 8006 on PowerEdge. Verify the SSH host key before using the tunnel."
  type        = number
  default     = null
}

module "poweredge_hypervisor" {
  source = "../../modules/poweredge/services/poweredge_hypervisor"
}

provider "proxmox" {
  endpoint = var.poweredge_api_tunnel_port == null ? "https://${module.poweredge_hypervisor.value.hostname}:8006/" : "https://127.0.0.1:${var.poweredge_api_tunnel_port}/"
  insecure = var.poweredge_api_tunnel_port != null
  username = "root@pam"
  password = var.poweredge_proxmox_root_password
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
