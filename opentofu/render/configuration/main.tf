variable "workspace" {
  type  = string
  const = true
}
module "configuration" {
  source    = "../../modules/configuration/workspace"
  workspace = var.workspace
}
output "lock_targets" { value = module.configuration.value.lock_targets }
