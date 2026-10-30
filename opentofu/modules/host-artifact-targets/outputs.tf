output "workspaces" { value = local.workspaces }

output "target" {
  value = one(values(local.targets))

  precondition {
    condition     = length(local.unnormalized_group_files) == 0
    error_message = "each group variable path in the descriptor must equal its normalized form: ${join(", ", local.unnormalized_group_files)}"
  }
}
