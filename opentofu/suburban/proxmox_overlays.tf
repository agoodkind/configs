# Scoped Proxmox privileges from agoodkind/proxmox-overlays.
#
# Stock Proxmox restricts container nesting and keyctl changes, VM vsock, and
# ACME account changes to root@pam. The overlay adds one privilege for each,
# and the roles below grant them to the automation user.
#
# OpenTofu declares the `pve-overlay` script and its patches as files on the
# hypervisor. `pve-overlay apply` then writes patched copies of the installed
# Proxmox modules to /etc/perl. The content of those copies depends on the
# installed Proxmox version, and the script generates them on the host.
locals {
  # A new commit here changes the declared files and reruns `apply`.
  proxmox_overlays_commit = "bb4324db0f2bf3caef9cd5569e1b3b0afb411aea"
  proxmox_overlays_source = "https://raw.githubusercontent.com/agoodkind/proxmox-overlays/${local.proxmox_overlays_commit}"
  proxmox_overlays_script = "/usr/local/sbin/pve-overlay"

  # Repository path of each file, with its path and mode on the hypervisor.
  # Every host path is in a directory that Debian creates.
  proxmox_overlays_files = {
    "pve-overlay" = {
      path        = local.proxmox_overlays_script
      permissions = "0755"
    }
    "patches/pve-access-control.patch" = {
      path        = "/usr/local/share/pve-overlay-pve-access-control.patch"
      permissions = "0644"
    }
    "patches/pve-container.patch" = {
      path        = "/usr/local/share/pve-overlay-pve-container.patch"
      permissions = "0644"
    }
    "patches/pve-manager.patch" = {
      path        = "/usr/local/share/pve-overlay-pve-manager.patch"
      permissions = "0644"
    }
    "patches/qemu-server.patch" = {
      path        = "/usr/local/share/pve-overlay-qemu-server.patch"
      permissions = "0644"
    }
  }
}

data "http" "proxmox_overlays" {
  for_each = local.proxmox_overlays_files

  url = "${local.proxmox_overlays_source}/${each.key}"

  lifecycle {
    postcondition {
      condition     = self.status_code == 200
      error_message = "GitHub returned ${self.status_code} for ${each.key} at ${local.proxmox_overlays_commit}."
    }
  }
}

resource "remote_file" "proxmox_overlays" {
  for_each = local.proxmox_overlays_files

  conn {
    # The provider joins host and port with a colon, and an IPv6 address
    # needs brackets there.
    host  = "[${local.service_mapping.suburban_hypervisor.ipv6}]"
    user  = "root"
    agent = true
  }

  path        = each.value.path
  content     = data.http.proxmox_overlays[each.key].response_body
  permissions = each.value.permissions
}

# Generates the patched module copies. It reruns when a declared file changes.
# A dpkg hook that `apply` installs reruns it after each package upgrade.
resource "terraform_data" "proxmox_overlays_apply" {
  triggers_replace = [
    for name in sort(keys(remote_file.proxmox_overlays)) :
    sha256(remote_file.proxmox_overlays[name].content)
  ]

  connection {
    type  = "ssh"
    host  = local.service_mapping.suburban_hypervisor.ipv6
    user  = "root"
    agent = true
  }

  provisioner "remote-exec" {
    inline = ["${local.proxmox_overlays_script} apply"]
  }
}

# Change `nesting` and `keyctl` on a container, also a privileged one.
resource "proxmox_virtual_environment_role" "scoped_container_features" {
  provider = proxmox.root
  role_id  = "ScopedContainerFeatures"

  privileges = [
    "VM.Config.Nesting",
    "VM.Config.Keyctl",
  ]

  # Proxmox rejects a role with a privilege that the overlay has not added.
  depends_on = [terraform_data.proxmox_overlays_apply]
}

# Enable the virtio vsock device on a VM.
resource "proxmox_virtual_environment_role" "scoped_vsock" {
  provider = proxmox.root
  role_id  = "ScopedVsock"

  privileges = [
    "VM.Config.Vsock",
  ]

  depends_on = [terraform_data.proxmox_overlays_apply]
}

# Read, register, update, and remove an ACME account.
resource "proxmox_virtual_environment_role" "scoped_acme_account" {
  provider = proxmox.root
  role_id  = "ScopedAcmeAccount"

  privileges = [
    "Sys.ACME.Account.Audit",
    "Sys.ACME.Account.Create",
    "Sys.ACME.Account.Modify",
    "Sys.ACME.Account.Remove",
  ]

  depends_on = [terraform_data.proxmox_overlays_apply]
}

resource "proxmox_acl" "scoped_container_features" {
  provider  = proxmox.root
  path      = "/vms"
  role_id   = proxmox_virtual_environment_role.scoped_container_features.role_id
  user_id   = var.automation_user
  propagate = true
}

resource "proxmox_acl" "scoped_vsock" {
  provider  = proxmox.root
  path      = "/vms"
  role_id   = proxmox_virtual_environment_role.scoped_vsock.role_id
  user_id   = var.automation_user
  propagate = true
}

resource "proxmox_acl" "scoped_acme_account" {
  provider  = proxmox.root
  path      = "/acme/accounts/${var.acme_account}"
  role_id   = proxmox_virtual_environment_role.scoped_acme_account.role_id
  user_id   = var.automation_user
  propagate = true
}

# Read, add, change, and delete one DNS plugin, and set its credentials. The
# role omits Sys.ACME.Plugin.Secret.Audit: OpenTofu writes the credentials and
# never reads them back.
resource "proxmox_virtual_environment_role" "scoped_acme_plugin" {
  provider = proxmox.root
  role_id  = "ScopedAcmePlugin"

  privileges = [
    "Sys.ACME.Plugin.Audit",
    "Sys.ACME.Plugin.Create",
    "Sys.ACME.Plugin.Modify",
    "Sys.ACME.Plugin.Remove",
    "Sys.ACME.Plugin.Secret.Modify",
  ]

  depends_on = [terraform_data.proxmox_overlays_apply]
}

resource "proxmox_acl" "scoped_acme_plugin" {
  provider  = proxmox.root
  path      = "/acme/plugins/${var.acme_plugin}"
  role_id   = proxmox_virtual_environment_role.scoped_acme_plugin.role_id
  user_id   = var.automation_user
  propagate = true
}

# Order, renew, and revoke the node certificate, and read and change the ACME
# options of the node config.
resource "proxmox_virtual_environment_role" "scoped_acme_certificate" {
  provider = proxmox.root
  role_id  = "ScopedAcmeCertificate"

  privileges = [
    "Sys.ACME.Certificate.Order",
    "Sys.ACME.Certificate.Renew",
    "Sys.ACME.Certificate.Revoke",
    "Sys.ACME.Config.Audit",
    "Sys.ACME.Config.Account.Modify",
    "Sys.ACME.Config.Domain.Modify",
    "Sys.ACME.Config.Domain.Remove",
  ]

  depends_on = [terraform_data.proxmox_overlays_apply]
}

resource "proxmox_acl" "scoped_acme_certificate" {
  provider  = proxmox.root
  path      = "/nodes/hypervisor"
  role_id   = proxmox_virtual_environment_role.scoped_acme_certificate.role_id
  user_id   = var.automation_user
  propagate = true
}
