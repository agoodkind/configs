# Scoped privileges for the automation principal on the suburban testbed host.
# The privileges exist only after `./configsctl deploy deploy-proxmox --limit
# suburban` applies agoodkind/proxmox-overlays. Proxmox rejects a role that
# lists an unknown privilege.
#
# Stock Proxmox restricts these operations to root@pam. Each role grants one
# of them, and each ACL entry grants a role to the automation user.

# Change `nesting` and `keyctl` on a container, also a privileged one.
resource "proxmox_virtual_environment_role" "suburban_container_features" {
  provider = proxmox.suburban_root
  role_id  = "ScopedContainerFeatures"

  privileges = [
    "VM.Config.Nesting",
    "VM.Config.Keyctl",
  ]
}

# Enable the virtio vsock device on a VM.
resource "proxmox_virtual_environment_role" "suburban_vsock" {
  provider = proxmox.suburban_root
  role_id  = "ScopedVsock"

  privileges = [
    "VM.Config.Vsock",
  ]
}

# Read, register, update, and remove an ACME account.
resource "proxmox_virtual_environment_role" "suburban_acme_account" {
  provider = proxmox.suburban_root
  role_id  = "ScopedAcmeAccount"

  privileges = [
    "Sys.ACME.Account.Audit",
    "Sys.ACME.Account.Create",
    "Sys.ACME.Account.Modify",
    "Sys.ACME.Account.Remove",
  ]
}

resource "proxmox_acl" "suburban_container_features" {
  provider  = proxmox.suburban_root
  path      = "/vms"
  role_id   = proxmox_virtual_environment_role.suburban_container_features.role_id
  user_id   = local.shared_vars.proxmox_api_user
  propagate = true
}

resource "proxmox_acl" "suburban_vsock" {
  provider  = proxmox.suburban_root
  path      = "/vms"
  role_id   = proxmox_virtual_environment_role.suburban_vsock.role_id
  user_id   = local.shared_vars.proxmox_api_user
  propagate = true
}

resource "proxmox_acl" "suburban_acme_account" {
  provider  = proxmox.suburban_root
  path      = "/acme/accounts/${local.acme_account}"
  role_id   = proxmox_virtual_environment_role.suburban_acme_account.role_id
  user_id   = local.shared_vars.proxmox_api_user
  propagate = true
}
