variable "workspace" {
  type  = string
  const = true
}

locals {
  sources = {
    "suburban/tack-qa" = "../tack/qa"
    "vault/tack-prod"  = "../tack/prod"
  }
}

module "selected" {
  source = lookup(local.sources, var.workspace, "../../${var.workspace}/configuration")
}

output "value" { value = module.selected }
