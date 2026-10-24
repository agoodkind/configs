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

variable "kernel_modules" {
  description = "A null value disables kernel module API access for the automation user by removing the ScopedKernelModules role and its ACL. An object grants ScopedKernelModules on /. The API accepts only entries in allow. An empty allow list permits no modules. The selected overlay commit must provide the kernel module API."
  type = object({
    allow = list(string)
  })
  default = null

  validation {
    condition = (
      var.kernel_modules == null
      ? true
      : alltrue([for name in var.kernel_modules.allow : can(regex("^[a-z0-9_]+$", name))])
    )
    error_message = "Each kernel module name must contain at least one character and use only lowercase letters (a-z), digits (0-9), or underscores."
  }
}

variable "container_options" {
  description = "An object grants bpfdelegate and hostnic privileges to the automation user on the selected containers and links. Container ACLs include retained_roles and existing VM roles. A null value removes ScopedBpfDelegate, ScopedHostNic, and ScopedHostNicUse."
  type = object({
    retained_roles = list(string)
    hostnic = object({
      vmids = list(number)
      links = list(string)
    })
    bpfdelegate = object({
      vmids   = list(number)
      cmds    = list(string)
      maps    = list(string)
      progs   = list(string)
      attachs = list(string)
    })
  })
  default = null

  validation {
    condition = (
      var.container_options == null
      ? true
      : alltrue([
        for role in var.container_options.retained_roles : can(regex("^[A-Za-z0-9_-]+$", role))
      ])
    )
    error_message = "Each retained_roles entry must contain at least one character and use only letters (A-Z or a-z), digits (0-9), underscores, or hyphens."
  }

  validation {
    condition = (
      var.container_options == null
      ? true
      : alltrue([
        for vmid in concat(
          var.container_options.hostnic.vmids,
          var.container_options.bpfdelegate.vmids,
        ) : vmid >= 100 && floor(vmid) == vmid
      ])
    )
    error_message = "Each hostnic.vmids or bpfdelegate.vmids entry must be an integer greater than or equal to 100."
  }

  validation {
    condition = (
      var.container_options == null
      ? true
      : alltrue([
        for link in var.container_options.hostnic.links : can(regex("^[A-Za-z0-9_.-]+$", link))
      ])
    )
    error_message = "Each hostnic.links entry must contain at least one character and use only letters (A-Z or a-z), digits (0-9), underscores, periods, or hyphens."
  }

  validation {
    condition = (
      var.container_options == null
      ? true
      : alltrue([
        for token in concat(
          var.container_options.bpfdelegate.cmds,
          var.container_options.bpfdelegate.maps,
          var.container_options.bpfdelegate.progs,
          var.container_options.bpfdelegate.attachs,
        ) : can(regex("^[a-z0-9_]+$", token))
      ])
    )
    error_message = "Each bpfdelegate.cmds, bpfdelegate.maps, bpfdelegate.progs, or bpfdelegate.attachs entry must contain at least one character and use only lowercase letters (a-z), digits (0-9), or underscores."
  }
}

variable "grub_cmdline_linux_default" {
  description = "The module writes GRUB_CMDLINE_LINUX_DEFAULT to /etc/default/grub.d/boot-messages.cfg and runs update-grub and proxmox-boot-tool refresh after file changes. `null` writes no file and runs no command."
  type        = string
  default     = null

  validation {
    condition = (
      var.grub_cmdline_linux_default == null
      ? true
      : !can(regex("[\"\\n\\\\$`]", var.grub_cmdline_linux_default))
    )
    error_message = "The value must not contain a double quote, a newline, a backslash, a dollar sign, or a backquote because a shell reads the file."
  }
}

variable "network_interfaces_file" {
  description = "The module writes the file at this local path to /etc/network/interfaces on the host. A null value writes no file."
  type        = string
  default     = null
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

  kernel_modules_allow = var.kernel_modules == null ? [] : var.kernel_modules.allow

  kernel_modules_roles = var.kernel_modules != null ? {
    ScopedKernelModules = {
      path = "/"
      privileges = [
        "Sys.KernelModules.Audit",
        "Sys.KernelModules.Modify",
      ]
    }
  } : {}

  bpfdelegate_privilege_prefixes = {
    cmds    = "VM.Config.BPFDelegate.Cmd"
    maps    = "VM.Config.BPFDelegate.Map"
    progs   = "VM.Config.BPFDelegate.Prog"
    attachs = "VM.Config.BPFDelegate.Attach"
  }

  bpfdelegate_privileges = var.container_options == null ? [] : flatten([
    for list_name in ["cmds", "maps", "progs", "attachs"] : [
      for token in var.container_options.bpfdelegate[list_name] : join("", [
        "${local.bpfdelegate_privilege_prefixes[list_name]}.",
        join("", [for part in split("_", token) : "${upper(substr(part, 0, 1))}${substr(part, 1, -1)}"]),
      ])
    ]
  ])

  container_options_role_names = ["ScopedBpfDelegate", "ScopedHostNic", "ScopedHostNicUse"]

  container_options_roles = var.container_options != null ? {
    ScopedBpfDelegate = {
      paths      = []
      privileges = local.bpfdelegate_privileges
    }
    ScopedHostNic = {
      paths = []
      privileges = [
        "VM.Config.HostNIC",
      ]
    }
    ScopedHostNicUse = {
      paths = [for link in var.container_options.hostnic.links : "/hostnic/${link}"]
      privileges = [
        "Sys.HostNIC.Use",
      ]
    }
  } : {}

  roles = merge(local.guest_api_roles, local.kernel_modules_roles, local.container_options_roles, {
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

  container_options_vm_roles = var.container_options == null ? {} : {
    ScopedHostNic     = var.container_options.hostnic.vmids
    ScopedBpfDelegate = var.container_options.bpfdelegate.vmids
  }

  container_options_vmids = sort(distinct(flatten([
    for role, vmids in local.container_options_vm_roles : [for vmid in vmids : tostring(vmid)]
  ])))

  # Each container ACL needs the existing VM roles because a more specific user ACL overrides inherited user roles.
  root_vm_roles = [
    for role in sort(keys(local.roles)) : role
    if try(local.roles[role].path, "") == "/" && anytrue([
      for privilege in local.roles[role].privileges : startswith(privilege, "VM.")
    ])
  ]

  container_options_acl_commands = [
    for vmid in local.container_options_vmids : join(" ", [
      "pveum acl modify /vms/${vmid} --users ${var.automation_user} --roles",
      join(",", concat(var.container_options.retained_roles, local.root_vm_roles, [
        for role in sort(keys(local.container_options_vm_roles)) : role
        if contains([for id in local.container_options_vm_roles[role] : tostring(id)], vmid)
      ])),
      "--propagate 1",
    ])
  ]

  # `pveum role add` fails for an existing role, and `pveum role modify` fails
  # for a missing role.
  grant_commands = concat(flatten([
    for role in sort(keys(local.roles)) : [
      join(" ", [
        "if pveum role list --output-format json | grep -q '\"roleid\":\"${role}\"';",
        "then pveum role modify ${role} --privs '${join(" ", local.roles[role].privileges)}';",
        "else pveum role add ${role} --privs '${join(" ", local.roles[role].privileges)}'; fi",
      ]),
      [
        for path in try(local.roles[role].paths, [local.roles[role].path]) :
        "pveum acl modify ${path} --users ${var.automation_user} --roles ${role} --propagate 1"
      ],
    ]
  ]), local.container_options_acl_commands)

  # Earlier grants put these roles on /vms and /nodes/<node>. `pveum acl
  # delete` succeeds when the entry is absent.
  guest_api_revoke_commands = var.guest_api ? [] : [
    join(" ", [
      "if pveum role list --output-format json | grep -q '\"roleid\":\"ScopedGuestExec\"';",
      "then pveum acl delete / --users ${var.automation_user} --roles ScopedGuestExec;",
      "pveum role delete ScopedGuestExec; fi",
    ]),
  ]

  kernel_modules_revoke_commands = var.kernel_modules != null ? [] : [
    join(" ", [
      "if pveum role list --output-format json | grep -q '\"roleid\":\"ScopedKernelModules\"';",
      "then pveum acl delete / --users ${var.automation_user} --roles ScopedKernelModules;",
      "pveum role delete ScopedKernelModules; fi",
    ]),
  ]

  container_options_revoke_commands = var.container_options != null ? [] : [
    for role in local.container_options_role_names : join(" ", [
      "if pveum role list --output-format json | grep -q '\"roleid\":\"${role}\"';",
      "then pveum acl delete / --users ${var.automation_user} --roles ${role};",
      "pveum role delete ${role}; fi",
    ])
  ]

  revoke_commands = concat([
    "pveum acl delete /vms --users ${var.automation_user} --roles ScopedContainerFeatures,ScopedVsock",
    "pveum acl delete /nodes/${var.node_name} --users ${var.automation_user} --roles ScopedAcmeCertificate",
  ], local.guest_api_revoke_commands, local.kernel_modules_revoke_commands, local.container_options_revoke_commands)

  allowlist_path = "/etc/pve-overlay/kernel-modules.allow"

  grub_boot_messages_path = "/etc/default/grub.d/boot-messages.cfg"

  network_interfaces_path = "/etc/network/interfaces"
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

resource "terraform_data" "allowlist_directory" {
  count = length(local.kernel_modules_allow) > 0 ? 1 : 0

  connection {
    type  = "ssh"
    host  = var.ssh_host
    user  = "root"
    agent = true
  }

  provisioner "remote-exec" {
    inline = ["install -d -m 0755 -o root -g root ${dirname(local.allowlist_path)}"]
  }
}

# The API treats a missing allowlist file as an empty allowlist.
resource "remote_file" "kernel_modules_allow" {
  count = length(local.kernel_modules_allow) > 0 ? 1 : 0

  conn {
    host  = local.remote_host
    user  = "root"
    agent = true
  }

  path        = local.allowlist_path
  content     = "${join("\n", local.kernel_modules_allow)}\n"
  permissions = "0644"

  depends_on = [terraform_data.allowlist_directory]
}

resource "remote_file" "grub_boot_messages" {
  count = var.grub_cmdline_linux_default != null ? 1 : 0

  conn {
    host  = local.remote_host
    user  = "root"
    agent = true
  }

  path        = local.grub_boot_messages_path
  content     = "GRUB_CMDLINE_LINUX_DEFAULT=\"${var.grub_cmdline_linux_default}\"\n"
  permissions = "0644"
}

resource "terraform_data" "grub_refresh" {
  count = var.grub_cmdline_linux_default != null ? 1 : 0

  triggers_replace = [
    sha256(remote_file.grub_boot_messages[0].content),
  ]

  connection {
    type  = "ssh"
    host  = var.ssh_host
    user  = "root"
    agent = true
  }

  provisioner "remote-exec" {
    script = "${path.module}/files/refresh-grub.sh"
  }
}

# The module does not run ifreload because a reload can interrupt the management address.
resource "remote_file" "network_interfaces" {
  count = var.network_interfaces_file != null ? 1 : 0

  conn {
    host  = local.remote_host
    user  = "root"
    agent = true
  }

  path        = local.network_interfaces_path
  content     = file(var.network_interfaces_file)
  permissions = "0644"
}
