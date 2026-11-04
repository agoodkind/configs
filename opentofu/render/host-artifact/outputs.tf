output "projection" {
  value = {
    workspace       = var.workspace
    node            = module.targets.target.node
    environment     = module.targets.target.environment
    validation      = module.targets.target.validation
    vars            = module.configuration.artifact_vars
    service_mapping = module.configuration.service_mapping
    directories     = module.targets.target.directories
    files           = module.targets.target.files
    programs        = try(module.targets.target.programs, [])
  }

  precondition {
    condition     = contains(module.targets.workspaces, var.workspace)
    error_message = "the repository does not contain a host artifact target descriptor for this workspace. Known selectors: ${join(", ", module.targets.workspaces)}."
  }
}
