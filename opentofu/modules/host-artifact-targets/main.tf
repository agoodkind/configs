# Each host artifact target has a host-artifact-target.json file in the
# OpenTofu directory of its exact workspace selector. The module lists those
# files without reading them and skips a path with a directory that starts
# with a dot or is named terraform.d or modules, as configsctl does.
# The helper renders a template:<path> source as a Jinja template. It copies
# an asset:<repository>:<path> source byte for byte from the verified
# checkout of that repository. The repository agoodkind/configs means this
# repository.
locals {
  workspaces_dir  = "${var.repository_root}/opentofu"
  descriptor_name = "host-artifact-target.json"
  excluded_names  = ["terraform.d", "modules"]

  descriptor_directories = [
    for path in fileset(local.workspaces_dir, "**/${local.descriptor_name}") : dirname(path)
  ]
  workspaces = sort([
    for directory in local.descriptor_directories : directory
    if alltrue([
      for part in split("/", directory) : !startswith(part, ".") && !contains(local.excluded_names, part)
    ])
  ])

  # The module reads the descriptor and the group variable files only for the
  # selected workspace. An invalid descriptor or a missing group file of another
  # target does not fail the build of the selected target.
  descriptors = {
    for workspace in local.workspaces :
    workspace => jsondecode(file("${local.workspaces_dir}/${workspace}/${local.descriptor_name}"))
    if workspace == var.workspace
  }
  targets = {
    for workspace, descriptor in local.descriptors : workspace => merge(descriptor, {
      vars = merge([
        for group_file in descriptor.group_vars : yamldecode(file("${var.repository_root}/${group_file}"))
      ]...)
    })
  }
}
