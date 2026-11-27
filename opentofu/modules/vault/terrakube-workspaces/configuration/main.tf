output "service_mapping" { value = {} }

output "managed_guests" { value = {} }

output "lock_targets" {
  value = {
    lock_free_types = [
      "terrakube_organization_template",
      "terrakube_workspace_variable",
      "terrakube_workspace_vcs",
    ]
  }
}
