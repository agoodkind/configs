output "workspaces" { value = local.workspaces }

output "target" {
  value = one(values(local.targets))

  precondition {
    condition     = length(local.unnormalized_configuration_files) == 0
    error_message = "each native configuration dependency path in the descriptor must equal its normalized form: ${join(", ", local.unnormalized_configuration_files)}"
  }
}
