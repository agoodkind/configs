# Scoped Proxmox privileges from agoodkind/proxmox-overlays on one hypervisor.
#
# Stock Proxmox restricts container nesting and keyctl changes, VM vsock, and
# ACME changes to root@pam. The overlay adds one privilege for each operation,
# and the roles below grant them to the automation user.
#
# OpenTofu declares the `pve-overlay` script and its patches as files on the
# hypervisor. `pve-overlay apply` then writes patched copies of the installed
# Proxmox modules to /etc/perl. The content of those copies depends on the
# installed Proxmox version, and the script generates them on the host.
#
# Every step uses root SSH through the SSH agent. Role and ACL changes need an
# administrator, and `pveum` on the host runs as root.
terraform {
  required_providers {
    http = {
      source  = "hashicorp/http"
      version = ">= 3.0"
    }
    # The registry has no signing key for this provider.
    remote = {
      source  = "tenstad/remote"
      version = ">= 0.2.1"
    }
  }
}

variable "ssh_host" {
  description = "Host name or address of the hypervisor for root SSH."
  type        = string
}

variable "node_name" {
  description = "Proxmox node name of the hypervisor."
  type        = string
}

variable "automation_user" {
  description = "Proxmox user that receives the scoped roles."
  type        = string
}

variable "acme_account" {
  description = "Name of the ACME account that the automation user manages."
  type        = string
}

variable "acme_plugin" {
  description = "Id of the ACME DNS plugin that the automation user manages."
  type        = string
}

locals {
  # A new commit here changes the declared files and reruns `apply`.
  commit = "bb4324db0f2bf3caef9cd5569e1b3b0afb411aea"
  source = "https://raw.githubusercontent.com/agoodkind/proxmox-overlays/${local.commit}"
  script = "/usr/local/sbin/pve-overlay"

  # Repository path of each file, with its path and mode on the hypervisor.
  # Every host path is in a directory that Debian creates.
  files = {
    "pve-overlay" = {
      path        = local.script
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

  # The remote provider joins host and port with a colon, and an IPv6 address
  # needs brackets there.
  remote_host = strcontains(var.ssh_host, ":") ? "[${var.ssh_host}]" : var.ssh_host

  # Each role with the ACL path of its grant.
  roles = {
    # Change `nesting` and `keyctl` on a container, also a privileged one.
    ScopedContainerFeatures = {
      path = "/vms"
      privileges = [
        "VM.Config.Nesting",
        "VM.Config.Keyctl",
      ]
    }
    # Enable the virtio vsock device on a VM.
    ScopedVsock = {
      path = "/vms"
      privileges = [
        "VM.Config.Vsock",
      ]
    }
    # Read, register, update, and remove one ACME account.
    ScopedAcmeAccount = {
      path = "/acme/accounts/${var.acme_account}"
      privileges = [
        "Sys.ACME.Account.Audit",
        "Sys.ACME.Account.Create",
        "Sys.ACME.Account.Modify",
        "Sys.ACME.Account.Remove",
      ]
    }
    # Read, add, change, and delete one DNS plugin, and set its credentials.
    # The role omits Sys.ACME.Plugin.Secret.Audit: OpenTofu writes the
    # credentials and never reads them back.
    ScopedAcmePlugin = {
      path = "/acme/plugins/${var.acme_plugin}"
      privileges = [
        "Sys.ACME.Plugin.Audit",
        "Sys.ACME.Plugin.Create",
        "Sys.ACME.Plugin.Modify",
        "Sys.ACME.Plugin.Remove",
        "Sys.ACME.Plugin.Secret.Modify",
      ]
    }
    # Order, renew, and revoke the node certificate, and read and change the
    # ACME options of the node config.
    ScopedAcmeCertificate = {
      path = "/nodes/${var.node_name}"
      privileges = [
        "Sys.ACME.Certificate.Order",
        "Sys.ACME.Certificate.Renew",
        "Sys.ACME.Certificate.Revoke",
        "Sys.ACME.Config.Audit",
        "Sys.ACME.Config.Account.Modify",
        "Sys.ACME.Config.Domain.Modify",
        "Sys.ACME.Config.Domain.Remove",
      ]
    }
  }

  # `pveum role add` rejects an existing role, and `pveum role modify` rejects
  # a missing one.
  grant_commands = flatten([
    for role in sort(keys(local.roles)) : [
      join(" ", [
        "if pveum role list --output-format json | grep -q '\"roleid\":\"${role}\"';",
        "then pveum role modify ${role} --privs '${join(" ", local.roles[role].privileges)}';",
        "else pveum role add ${role} --privs '${join(" ", local.roles[role].privileges)}'; fi",
      ]),
      "pveum acl modify ${local.roles[role].path} --users ${var.automation_user} --roles ${role} --propagate 1",
    ]
  ])
}

data "http" "files" {
  for_each = local.files

  url = "${local.source}/${each.key}"

  lifecycle {
    postcondition {
      condition     = self.status_code == 200
      error_message = "GitHub returned ${self.status_code} for ${each.key} at ${local.commit}."
    }
  }
}

resource "remote_file" "files" {
  for_each = local.files

  conn {
    host  = local.remote_host
    user  = "root"
    agent = true
  }

  path        = each.value.path
  content     = data.http.files[each.key].response_body
  permissions = each.value.permissions
}

# Generates the patched module copies. It reruns when a declared file changes.
# A dpkg hook that `apply` installs reruns it after each package upgrade.
resource "terraform_data" "apply" {
  triggers_replace = [
    for name in sort(keys(remote_file.files)) :
    sha256(remote_file.files[name].content)
  ]

  connection {
    type  = "ssh"
    host  = var.ssh_host
    user  = "root"
    agent = true
  }

  provisioner "remote-exec" {
    inline = ["${local.script} apply"]
  }
}

# Creates the roles and grants them to the automation user. Proxmox rejects a
# role with a privilege that the overlay has not added.
resource "terraform_data" "grant" {
  triggers_replace = [
    terraform_data.apply.id,
    sha256(join("\n", local.grant_commands)),
  ]

  connection {
    type  = "ssh"
    host  = var.ssh_host
    user  = "root"
    agent = true
  }

  provisioner "remote-exec" {
    inline = concat(["set -eu"], local.grant_commands)
  }
}
