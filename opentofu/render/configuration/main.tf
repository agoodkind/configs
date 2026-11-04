variable "workspace" {
  type  = string
  const = true
}
module "configuration" { source = "../../modules/${var.workspace}/configuration" }
output "lock_targets" { value = module.configuration.lock_targets }
