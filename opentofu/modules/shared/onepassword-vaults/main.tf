terraform {
  required_version = ">= 1.11"
  required_providers {
    opadmin = {
      source  = "registry.opentofu.org/pleme-io/opadmin"
      version = "0.1.0"
    }
  }
}

variable "services" {
  description = "Each service maps to the environments that require separate vaults."
  type        = map(set(string))
  nullable    = false

  validation {
    condition     = alltrue([for service in keys(var.services) : can(regex("^[a-z][a-z0-9-]*$", service))])
    error_message = "Service names must start with a lowercase letter and contain only lowercase letters, digits, or hyphens."
  }

  validation {
    condition     = alltrue([for environments in values(var.services) : environments != null ? length(environments) > 0 : false])
    error_message = "Each service must declare at least one environment."
  }

  validation {
    condition = alltrue(flatten([
      for environments in values(var.services) : [
        for environment in coalesce(environments, toset([])) : can(regex("^[a-z][a-z0-9-]*$", environment))
      ]
    ]))
    error_message = "Environment names must start with a lowercase letter and contain only lowercase letters, digits, or hyphens."
  }
}

locals {
  vaults = merge({}, [
    for service, environments in var.services : {
      for environment in coalesce(environments, toset([])) : "${service}/${environment}" => {
        service     = service
        environment = environment
      } if environment != null
    }
  ]...)
}

resource "opadmin_vault" "service" {
  for_each = local.vaults

  title               = each.key
  description         = "Credentials for ${each.value.service} in ${each.value.environment}."
  allow_admins_access = true
  force_destroy       = false

  lifecycle {
    prevent_destroy = true
  }
}

output "vault_ids" {
  description = "Vault IDs grouped by service and environment."
  value = {
    for service, environments in var.services : service => {
      for environment in coalesce(environments, toset([])) : environment => opadmin_vault.service["${service}/${environment}"].id
      if environment != null
    }
  }
}
