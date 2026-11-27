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

variable "grant_scope" {
  description = "Select all bootstrap grants or only the host artifact role."
  type        = string
  default     = "all"

  validation {
    condition     = contains(["all", "host-artifacts"], var.grant_scope)
    error_message = "grant_scope must be all or host-artifacts."
  }
}

variable "kernel_modules" {
  description = <<-EOT
    The selected overlay commit must include the kernel module API. The API accepts
    only the module names in allow. An empty list blocks all module names.

    When grant_scope is "all", the overlay manages the ScopedKernelModules role
    for the automation user. If kernel_modules is null, the overlay removes the
    role. Otherwise, it grants the role on the Proxmox root path (/).
  EOT
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

variable "base_patch_sources" {
  description = "Add or replace the ordered list of repository-relative .patch paths for each flat patch destination."
  type        = map(list(string))
  default     = {}
  nullable    = false

  validation {
    condition = alltrue([
      for destination in keys(var.base_patch_sources) :
      can(regex("^patches/[A-Za-z0-9][A-Za-z0-9_-]*\\.patch$", destination))
    ])
    error_message = "Each base_patch_sources key must have the form patches/<name>.patch. Start the name with a letter or digit and use only letters, digits, underscores, and hyphens."
  }

  validation {
    condition = alltrue([
      for sources in values(var.base_patch_sources) :
      sources != null ? length(sources) > 0 && alltrue([
        for source in sources : can(regex("^([A-Za-z0-9_-][A-Za-z0-9_.-]*/)*[A-Za-z0-9_-][A-Za-z0-9_.-]*\\.patch$", source))
      ]) : false
    ])
    error_message = "Each base_patch_sources value must be a nonempty list of repository-relative .patch paths without empty, dot, or parent directory segments."
  }
}

variable "sriov" {
  description = "When grant_scope is all, true grants the SR-IOV and PCI mapping roles to the automation user and false removes those roles."
  type        = bool
  default     = false
}

variable "host_artifact_policies" {
  description = "The host_artifact_policies map contains host artifact policy documents from the host-artifacts module, keyed by the exact workspace selector. The module writes each document and the trusted root over root SSH and grants ScopedHostArtifacts to the automation user. An empty map does not cause the module to write a file or grant a role. The selected overlay commit must provide the host artifact API."
  type        = map(string)
  default     = {}
}

variable "acme_account" {
  description = "Name of the ACME account that the automation user manages."
  type        = string
}

variable "acme_plugin" {
  description = "Id of the ACME DNS plugin that the automation user manages."
  type        = string
}

variable "retain_node_acme_role" {
  description = "Set this value to true when another workspace manages the node certificate role."
  type        = bool
  default     = false
}

locals {
  source = "https://raw.githubusercontent.com/agoodkind/proxmox-overlays/${var.commit}"
  script = "/usr/local/sbin/pve-overlay"

  template_directory = "/usr/local/share/pve-overlay/templates"
  template_sources = toset([
    "templates/apt-hook.conf",
    "templates/upgrade-conflict.eml",
  ])

  file_sources = merge({
    "pve-overlay" = ["pve-overlay"]
    "patches/pve-access-control.patch" = [
      "patches/pve-access-control-10-acme.patch",
      "patches/pve-access-control-20-container-features.patch",
      "patches/pve-access-control-30-vsock.patch",
      "patches/pve-access-control-40-guest-api.patch",
      "patches/pve-access-control-50-bpfdelegate.patch",
      "patches/pve-access-control-60-hostnic.patch",
      "patches/pve-access-control-70-kernel-modules.patch",
      "patches/pve-access-control-80-host-artifacts.patch",
      "patches/pve-access-control-90-sriov.patch",
    ]
    "patches/pve-container.patch" = [
      "patches/pve-container-10-features.patch",
      "patches/pve-container-20-guest-api.patch",
      "patches/pve-container-30-bpfdelegate.patch",
      "patches/pve-container-40-hostnic.patch",
    ]
    "patches/pve-http-server.patch"  = ["patches/pve-http-server.patch"]
    "patches/pve-manager-acme.patch" = ["patches/pve-manager-10-acme.patch"]
    "patches/pve-manager.patch" = [
      "patches/pve-manager-20-kernel-modules.patch",
      "patches/pve-manager-30-host-artifacts.patch",
      "patches/pve-manager-31-host-secrets.patch",
      "patches/pve-manager-32-host-units.patch",
      "patches/pve-manager-33-installed-source.patch",
      "patches/pve-manager-40-sriov.patch",
    ]
    "patches/qemu-server.patch" = [
      "patches/qemu-server-10-vsock.patch",
      "patches/qemu-server-20-guest-api.patch",
      "patches/qemu-server-30-sriov.patch",
    ]
  }, var.base_patch_sources)

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

  host_artifacts_roles = local.host_artifacts_enabled ? {
    ScopedHostArtifacts = {
      path = "/nodes/${var.node_name}"
      privileges = [
        "Sys.HostArtifacts.Audit",
        "Sys.HostArtifacts.Modify",
        "Sys.HostSecrets.Modify",
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

  sriov_roles = {
    ScopedSRIOV = {
      path       = "/"
      privileges = ["Sys.SRIOV.Audit", "Sys.SRIOV.Modify"]
    }
    ScopedPCIMapping = {
      path       = "/mapping/pci"
      privileges = ["Mapping.Audit", "Mapping.Use", "Mapping.Modify"]
    }
  }

  roles = merge(var.sriov ? local.sriov_roles : {}, local.guest_api_roles, local.kernel_modules_roles, local.host_artifacts_roles, local.container_options_roles, {
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
    for role in keys(local.roles) : role
    if try(local.roles[role].path, "") == "/" && anytrue([
      for privilege in local.roles[role].privileges : startswith(privilege, "VM.")
    ])
  ]

  container_options_acl_commands = [
    for vmid in local.container_options_vmids : join(" ", [
      "pveum acl modify /vms/${vmid} --users ${var.automation_user} --roles",
      join(",", concat(var.container_options.retained_roles, local.root_vm_roles, [
        for role in keys(local.container_options_vm_roles) : role
        if contains([for id in local.container_options_vm_roles[role] : tostring(id)], vmid)
      ])),
      "--propagate 1",
    ])
  ]

  # `pveum role add` fails for an existing role, and `pveum role modify` fails
  # for a missing role.
  grant_commands = concat(flatten([
    for role in keys(local.roles) : [
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
    if length(local.roles[role].privileges) > 0 && (var.grant_scope == "all" || contains(keys(local.host_artifacts_roles), role))
  ]), var.grant_scope == "all" ? local.container_options_acl_commands : [])

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

  role_paths_to_revoke = merge(
    var.container_options != null ? {} : { for role in local.container_options_role_names : role => "/" },
    var.sriov ? {} : { for role, definition in local.sriov_roles : role => definition.path },
  )

  role_revoke_commands = [
    for role, path in local.role_paths_to_revoke : join(" ", [
      "if pveum role list --output-format json | grep -q '\"roleid\":\"${role}\"';",
      "then pveum acl delete ${path} --users ${var.automation_user} --roles ${role};",
      "pveum role delete ${role}; fi",
    ])
  ]

  revoke_commands = var.grant_scope == "all" ? concat([
    "pveum acl delete /vms --users ${var.automation_user} --roles ScopedContainerFeatures,ScopedVsock",
    ], var.retain_node_acme_role ? [] : [
    "pveum acl delete /nodes/${var.node_name} --users ${var.automation_user} --roles ScopedAcmeCertificate",
  ], local.guest_api_revoke_commands, local.kernel_modules_revoke_commands, local.role_revoke_commands) : []

  allowlist_path = "/etc/pve-overlay/kernel-modules.allow"

  # The installer reads only the policy of the requested target and the
  # trusted root. It requires root ownership of each file and each directory
  # above it. Group and other users cannot have write permission on them.
  host_artifacts_directory         = "/etc/pve-overlay/host-artifacts"
  host_artifacts_policy_directory  = "${local.host_artifacts_directory}/targets"
  host_artifacts_trusted_root_path = "${local.host_artifacts_directory}/trusted_root.jsonl"
  host_artifacts_enabled           = length(var.host_artifact_policies) > 0

  grub_boot_messages_path = "/etc/default/grub.d/boot-messages.cfg"

}

data "http" "files" {
  for_each = setunion(toset(flatten(values(local.file_sources))), local.template_sources)

  url = "${local.source}/${each.key}"

  lifecycle {
    postcondition {
      condition     = self.status_code == 200
      error_message = "GitHub returned ${self.status_code} for ${each.key} at ${var.commit}."
    }
  }
}

resource "terraform_data" "template_directory" {
  connection {
    type  = "ssh"
    host  = var.ssh_host
    user  = "root"
    agent = true
  }

  provisioner "remote-exec" {
    inline = ["install -d -m 0755 -o root -g root ${local.template_directory}"]
  }
}

resource "remote_file" "templates" {
  for_each = local.template_sources

  conn {
    host  = local.remote_host
    user  = "root"
    agent = true
  }

  path        = "${local.template_directory}/${basename(each.key)}"
  content     = data.http.files[each.key].response_body
  permissions = "0644"

  depends_on = [terraform_data.template_directory]
}

resource "remote_file" "files" {
  for_each = local.file_sources

  conn {
    host  = local.remote_host
    user  = "root"
    agent = true
  }

  path        = each.key == "pve-overlay" ? local.script : "/usr/local/share/pve-overlay-${trimprefix(each.key, "patches/")}"
  content     = join("", [for source in each.value : data.http.files[source].response_body])
  permissions = each.key == "pve-overlay" ? "0755" : "0644"

  # The dpkg hook can invoke the installed script during a package operation.
  depends_on = [remote_file.templates]
}

# The APT hook checks proposed packages before installation. The dpkg hook
# reapplies overlays after each package operation.
resource "terraform_data" "apply" {
  triggers_replace = [
    for file in merge(remote_file.files, remote_file.templates) : sha256(file.content)
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

  lifecycle {
    precondition {
      condition     = var.grant_scope != "host-artifacts" || local.host_artifacts_enabled
      error_message = "host-artifacts grant scope requires a nonempty host_artifact_policies map."
    }
  }

  depends_on = [remote_file.host_artifacts_trusted_root, remote_file.host_artifact_policies]
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

resource "terraform_data" "host_artifacts_directory" {
  count = local.host_artifacts_enabled ? 1 : 0

  connection {
    type  = "ssh"
    host  = var.ssh_host
    user  = "root"
    agent = true
  }

  provisioner "remote-exec" {
    inline = [join(" ", [
      "install -d -m 0755 -o root -g root",
      dirname(local.host_artifacts_directory),
      local.host_artifacts_directory,
      local.host_artifacts_policy_directory,
    ])]
  }
}

resource "remote_file" "host_artifacts_trusted_root" {
  count = local.host_artifacts_enabled ? 1 : 0

  conn {
    host  = local.remote_host
    user  = "root"
    agent = true
  }

  path        = local.host_artifacts_trusted_root_path
  content     = file("${path.module}/host-artifacts-trusted-root.jsonl")
  permissions = "0644"

  depends_on = [terraform_data.host_artifacts_directory]
}

# The module writes each policy file with the SHA-256 of the workspace
# selector as its file name.
resource "remote_file" "host_artifact_policies" {
  for_each = var.host_artifact_policies

  conn {
    host  = local.remote_host
    user  = "root"
    agent = true
  }

  path        = "${local.host_artifacts_policy_directory}/${sha256(each.key)}.json"
  content     = "${each.value}\n"
  permissions = "0644"

  depends_on = [terraform_data.host_artifacts_directory]
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

removed {
  from = remote_file.network_interfaces
  lifecycle { destroy = false }
}
