variable "workspace" {
  type  = string
  const = true
}

locals {
  sources = {
    "suburban/tack-qa" = "../tack/qa"
    "vault/tack-prod"  = "../tack/prod"
  }
  source_directory = lookup(local.sources, var.workspace, "../../${var.workspace}/configuration")
}

module "selected" {
  source = local.source_directory
}

output "value" { value = module.selected }

output "source_directory" { value = abspath("${path.module}/${local.source_directory}") }
