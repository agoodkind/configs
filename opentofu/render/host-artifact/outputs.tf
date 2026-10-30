output "projection" {
  value = {
    workspace       = var.workspace
    node            = module.targets.target.node
    environment     = module.targets.target.environment
    vars            = module.targets.target.vars
    service_mapping = module.contracts.service_mapping
    directories     = module.targets.target.directories
    files           = module.targets.target.files
  }

  precondition {
    condition     = contains(module.targets.workspaces, var.workspace)
    error_message = "the host artifact catalog does not contain this workspace. Known selectors: ${join(", ", module.targets.workspaces)}."
  }
}
