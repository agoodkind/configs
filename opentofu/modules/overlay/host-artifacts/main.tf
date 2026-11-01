# The module builds the host artifact policy of each selected workspace on one
# host from its host-artifact-target.json descriptor. It reads only the
# descriptors of the selected workspaces. The installer in pve-manager reads one
# policy for each request and does not compare policies with each other. The
# module fails the plan when the policies of the host overlap.
variable "repository_root" {
  description = "The repository_root is the absolute path of the repository that contains the opentofu directory with the descriptors."
  type        = string
}

variable "node_name" {
  description = "The node_name is the Proxmox node name of the host. Each selected descriptor must list this node."
  type        = string
}

variable "workspaces" {
  description = "The workspaces list contains the exact workspace selectors of the host artifact targets on the host."
  type        = list(string)
}

locals {
  # The configs CI workflow builds and attests every archive on main.
  configuration = {
    repository        = "agoodkind/configs"
    workflow_uri      = "https://github.com/agoodkind/configs/.github/workflows/ci.yml@refs/heads/main"
    source_ref        = "refs/heads/main"
    max_archive_bytes = 8 * 1024 * 1024
    max_bundle_bytes  = 1024 * 1024
  }

  # The module fixes the workflow, source ref, and size limits for all programs.
  program_configuration = {
    workflow_uri      = "https://github.com/agoodkind/go-makefile/.github/workflows/_package.yml@refs/heads/main"
    source_ref        = "refs/heads/main"
    max_archive_bytes = 64 * 1024 * 1024
    max_bundle_bytes  = 1024 * 1024
  }

  # The installer stores its policies and records in these directories.
  protected_directories = ["/etc/pve-overlay", "/var/lib/pve-overlay"]
  entry_keys            = ["path", "mode", "owner", "group"]

  # A clean absolute path has one or more components. Each component is
  # nonempty and differs from "." and "..".
  absolute_path_pattern   = "^(/[^/]+)+$"
  dot_component_pattern   = "/\\.\\.?(/|$)"
  missing_workspaces      = [for workspace in var.workspaces : workspace if module.targets[workspace].target == null]
  descriptor_workspaces   = [for workspace in var.workspaces : workspace if module.targets[workspace].target != null]
  descriptor_of_workspace = { for workspace in local.descriptor_workspaces : workspace => module.targets[workspace].target }

  # Policy programs omit the descriptor's asset and member fields.
  programs_of_workspace = {
    for workspace, descriptor in local.descriptor_of_workspace : workspace => [
      for entry in lookup(descriptor, "programs", []) : {
        name              = entry.name
        repository        = entry.repository
        workflow_uri      = local.program_configuration.workflow_uri
        source_ref        = local.program_configuration.source_ref
        path              = entry.path
        mode              = entry.mode
        owner             = entry.owner
        group             = entry.group
        max_archive_bytes = local.program_configuration.max_archive_bytes
        max_bundle_bytes  = local.program_configuration.max_bundle_bytes
      }
    ]
  }

  policies = {
    for workspace, descriptor in local.descriptor_of_workspace : workspace => merge({
      workspace     = workspace
      node          = descriptor.node
      environment   = descriptor.environment
      configuration = local.configuration
      directories = [
        for entry in descriptor.directories : {
          path  = entry.path
          mode  = entry.mode
          owner = entry.owner
          group = entry.group
        }
      ]
      parent_directories = [
        for entry in lookup(descriptor, "parent_directories", []) : {
          for key in local.entry_keys : key => entry[key] if contains(keys(entry), key)
        }
      ]
      files = [
        for entry in descriptor.files : {
          path  = entry.destination
          mode  = entry.mode
          owner = entry.owner
          group = entry.group
        }
      ]
      secret_fields = [
        for field in lookup(descriptor, "secret_fields", []) : {
          name           = field.name
          storage_path   = field.storage_path
          mode           = field.mode
          owner          = field.owner
          group          = field.group
          pattern        = field.pattern
          min_length     = field.min_length
          max_length     = field.max_length
          consumer_units = field.consumer_units
        }
      ]
      }, {
      # Include programs only when the descriptor lists at least one program.
      # Installed overlays reject unknown policy keys. Descriptors without programs
      # produce the same policy JSON as before.
      for key in ["programs"] : key => local.programs_of_workspace[workspace]
      if length(local.programs_of_workspace[workspace]) > 0
    })
  }

  paths_of_target = {
    for workspace, policy in local.policies : workspace => {
      owned    = distinct([for entry in policy.directories : entry.path])
      parents  = distinct([for entry in policy.parent_directories : entry.path])
      files    = distinct([for entry in policy.files : entry.path])
      secrets  = distinct([for field in policy.secret_fields : field.storage_path])
      programs = distinct([for entry in local.programs_of_workspace[workspace] : entry.path])
    }
  }

  all_paths = distinct(flatten([
    for workspace, paths in local.paths_of_target :
    concat(paths.owned, paths.parents, paths.files, paths.secrets, paths.programs)
  ]))

  unclean_paths = [
    for path in local.all_paths : path
    if !can(regex(local.absolute_path_pattern, path)) || can(regex(local.dot_component_pattern, path))
  ]

  protected_paths = [
    for path in local.all_paths : path
    if anytrue([
      for directory in local.protected_directories :
      path == directory || startswith(path, "${directory}/")
    ])
  ]

  wrong_node_workspaces = [
    for workspace, policy in local.policies : "${workspace} (${policy.node})"
    if policy.node != var.node_name
  ]

  # Each pair has two different workspaces. The list has both orders of each pair.
  target_pairs = [
    for pair in setproduct(keys(local.paths_of_target), keys(local.paths_of_target)) : pair
    if pair[0] != pair[1]
  ]

  owned_paths = flatten([for workspace, paths in local.paths_of_target : paths.owned])
  shared_owned_directories = [
    for path in distinct(local.owned_paths) :
    "${path} (${join(", ", [for workspace, paths in local.paths_of_target : workspace if contains(paths.owned, path)])})"
    if length([for owned in local.owned_paths : owned if owned == path]) > 1
  ]

  owned_and_inspected_directories = flatten([
    for pair in local.target_pairs : [
      for path in local.paths_of_target[pair[0]].owned :
      "${path} (owned by ${pair[0]}, inspected by ${pair[1]})"
      if contains(local.paths_of_target[pair[1]].parents, path)
    ]
  ])

  # The installer of one target can create, change, and remove its owned
  # directories. If one target owns a directory that contains a path of
  # another target, the other target depends on that directory without
  # listing it. Separate checks test equal owned and inspected directories.
  contained_paths = flatten([
    for pair in local.target_pairs : [
      for directory in local.paths_of_target[pair[0]].owned : [
        for path in concat(
          local.paths_of_target[pair[1]].files,
          local.paths_of_target[pair[1]].secrets,
          local.paths_of_target[pair[1]].programs,
          local.paths_of_target[pair[1]].owned,
          local.paths_of_target[pair[1]].parents,
        ) :
        "${path} of ${pair[1]} (inside ${directory} of ${pair[0]})"
        if startswith(path, "${directory}/") || (
          path == directory &&
          !contains(local.paths_of_target[pair[1]].owned, path) &&
          !contains(local.paths_of_target[pair[1]].parents, path)
        )
      ]
    ]
  ])

  leaf_paths = flatten([
    for workspace, paths in local.paths_of_target : distinct(concat(paths.files, paths.secrets, paths.programs))
  ])
  shared_leaf_paths = [
    for path in distinct(local.leaf_paths) :
    "${path} (${join(", ", [for workspace, paths in local.paths_of_target : workspace if contains(concat(paths.files, paths.secrets, paths.programs), path)])})"
    if length([for leaf in local.leaf_paths : leaf if leaf == path]) > 1
  ]

  # local.shared_leaf_paths compares targets using each target's distinct paths.
  # This check finds repeated program paths and paths that equal file destinations
  # or secret storage paths within one target.
  same_target_program_paths = flatten([
    for workspace, paths in local.paths_of_target : [
      for path in paths.programs :
      "${path} (${workspace})"
      if contains(paths.files, path) || contains(paths.secrets, path) || length([
        for entry in local.programs_of_workspace[workspace] : entry if entry.path == path
      ]) > 1
    ]
  ])
}

module "targets" {
  source   = "../../host-artifact-targets"
  for_each = toset(var.workspaces)

  repository_root = var.repository_root
  workspace       = each.key
}

output "policies" {
  description = "The policies map contains the JSON policy document of each selected workspace, keyed by the exact workspace selector."
  value       = { for workspace, policy in local.policies : workspace => jsonencode(policy) }

  precondition {
    condition     = length(local.missing_workspaces) == 0
    error_message = "the repository does not contain a host-artifact-target.json descriptor for these workspaces: ${join(", ", local.missing_workspaces)}"
  }

  precondition {
    condition     = length(local.wrong_node_workspaces) == 0
    error_message = "these descriptors list a node other than ${var.node_name}: ${join(", ", local.wrong_node_workspaces)}"
  }

  precondition {
    condition     = length(local.unclean_paths) == 0
    error_message = "each policy path must be absolute and cannot contain an empty, \".\", or \"..\" component: ${join(", ", local.unclean_paths)}"
  }

  precondition {
    condition     = length(local.protected_paths) == 0
    error_message = "a policy path cannot equal or be inside /etc/pve-overlay or /var/lib/pve-overlay: ${join(", ", local.protected_paths)}"
  }

  precondition {
    condition     = length(local.shared_owned_directories) == 0
    error_message = "two targets on one host cannot own the same directory: ${join("; ", local.shared_owned_directories)}"
  }

  precondition {
    condition     = length(local.owned_and_inspected_directories) == 0
    error_message = "a directory that one target owns cannot be an inspect-only parent of another target: ${join("; ", local.owned_and_inspected_directories)}"
  }

  precondition {
    condition     = length(local.contained_paths) == 0
    error_message = "an owned directory of one target cannot equal or contain a file, secret storage path, program path, owned directory, or inspect-only parent of another target: ${join("; ", local.contained_paths)}"
  }

  precondition {
    condition     = length(local.shared_leaf_paths) == 0
    error_message = "two targets on one host cannot list the same file destination, secret storage path, or program path: ${join("; ", local.shared_leaf_paths)}"
  }

  precondition {
    condition     = length(local.same_target_program_paths) == 0
    error_message = "a target cannot repeat a program path or use it as a file destination or secret storage path: ${join("; ", local.same_target_program_paths)}"
  }
}
