# `pve-overlay apply` writes patched copies of the installed Proxmox Perl
# modules to /etc/perl on the hypervisor. The patches add one privilege for
# each operation that Proxmox otherwise allows only for root@pam.
terraform {
  required_providers {
    http = {
      source  = "hashicorp/http"
      version = ">= 3.0"
    }
    # The OpenTofu registry has no signing key for tenstad/remote, and
    # `tofu init` skips the signature check for it.
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

variable "commit" {
  description = "The hypervisor installs this commit from agoodkind/proxmox-overlays."
  type        = string

  validation {
    condition     = can(regex("^[0-9a-f]{40}$", var.commit))
    error_message = "commit must contain exactly 40 lowercase hexadecimal characters."
  }
}

variable "guest_api" {
  description = "The automation user can execute guest commands and read and write guest files when this option is true. The selected commit must provide the guest API."
  type        = bool
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
  source = "https://raw.githubusercontent.com/agoodkind/proxmox-overlays/${var.commit}"
  script = "/usr/local/sbin/pve-overlay"

  # Key: the file path in agoodkind/proxmox-overlays. Debian creates
  # /usr/local/sbin and /usr/local/share.
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

  # remote_file builds the SSH address as <host>:<port>. An IPv6 address needs
  # brackets in that format.
  remote_host = strcontains(var.ssh_host, ":") ? "[${var.ssh_host}]" : var.ssh_host

  # Proxmox evaluates a user's privileges on a path from the most specific
  # path with an ACL entry for that user. An entry on /vms or /nodes/<node>
  # replaces every role granted to the user on /, including VM.Audit, and the
  # inventory plugin then lists no guest. The guest and node roles are granted
  # on / with the user's other roles.
  guest_api_roles = var.guest_api ? {
    ScopedGuestExec = {
      path = "/"
      privileges = [
        "VM.Guest.Exec",
        "VM.Guest.FileRead",
        "VM.Guest.FileWrite",
      ]
    }
  } : {}

  roles = merge(local.guest_api_roles, {
    ScopedContainerFeatures = {
      path = "/"
      privileges = [
        "VM.Config.Nesting",
        "VM.Config.Keyctl",
      ]
    }
    ScopedVsock = {
      path = "/"
      privileges = [
        "VM.Config.Vsock",
      ]
    }
    ScopedAcmeAccount = {
      path = "/acme/accounts/${var.acme_account}"
      privileges = [
        "Sys.ACME.Account.Audit",
        "Sys.ACME.Account.Create",
        "Sys.ACME.Account.Modify",
        "Sys.ACME.Account.Remove",
      ]
    }
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
    ScopedAcmeCertificate = {
      path = "/"
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
  })

  # `pveum role add` fails for an existing role, and `pveum role modify` fails
  # for a missing role.
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

  # Earlier grants put these roles on /vms and /nodes/<node>. `pveum acl
  # delete` succeeds when the entry is absent.
  guest_api_revoke_commands = var.guest_api ? [] : [
    join(" ", [
      "if pveum role list --output-format json | grep -q '\"roleid\":\"ScopedGuestExec\"';",
      "then pveum acl delete / --users ${var.automation_user} --roles ScopedGuestExec;",
      "pveum role delete ScopedGuestExec; fi",
    ]),
  ]

  revoke_commands = concat([
    "pveum acl delete /vms --users ${var.automation_user} --roles ScopedContainerFeatures,ScopedVsock",
    "pveum acl delete /nodes/${var.node_name} --users ${var.automation_user} --roles ScopedAcmeCertificate",
  ], local.guest_api_revoke_commands)
}

data "http" "files" {
  for_each = local.files

  url = "${local.source}/${each.key}"

  lifecycle {
    postcondition {
      condition     = self.status_code == 200
      error_message = "GitHub returned ${self.status_code} for ${each.key} at ${var.commit}."
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

# `pve-overlay apply` also installs a dpkg hook. The hook runs
# `pve-overlay apply` after each package operation on the hypervisor.
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

# terraform_data.grant runs after terraform_data.apply. `pveum` rejects a role
# with a privilege that `pve-overlay apply` has not added.
resource "terraform_data" "grant" {
  triggers_replace = [
    terraform_data.apply.id,
    sha256(join("\n", concat(local.grant_commands, local.revoke_commands))),
  ]

  connection {
    type  = "ssh"
    host  = var.ssh_host
    user  = "root"
    agent = true
  }

  provisioner "remote-exec" {
    inline = concat(["set -eu"], local.grant_commands, local.revoke_commands)
  }
}
