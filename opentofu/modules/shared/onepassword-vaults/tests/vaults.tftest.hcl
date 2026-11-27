mock_provider "opadmin" {}

run "separate_service_environments" {
  command = plan

  variables {
    services = {
      tack  = ["qa", "prod"]
      clyde = ["prod"]
    }
  }

  assert {
    condition     = toset(keys(opadmin_vault.service)) == toset(["tack/qa", "tack/prod", "clyde/prod"])
    error_message = "Each service and environment must have its own vault."
  }

  assert {
    condition     = alltrue([for key, vault in opadmin_vault.service : vault.title == key && !vault.force_destroy])
    error_message = "Each vault must retain its service and environment name and protect nonempty vaults from deletion."
  }
}

run "preserve_identifier_boundaries" {
  command = plan

  variables {
    services = {
      service     = ["api-prod"]
      service-api = ["prod"]
    }
  }

  assert {
    condition     = toset(keys(opadmin_vault.service)) == toset(["service/api-prod", "service-api/prod"])
    error_message = "Hyphens in service and environment names must not combine separate vaults."
  }
}

run "map_vault_ids" {
  command = plan

  variables {
    services = { tack = ["qa", "prod"] }
  }

  override_resource {
    target = opadmin_vault.service["tack/qa"]
    values = { id = "qa-vault-id" }
  }

  override_resource {
    target = opadmin_vault.service["tack/prod"]
    values = { id = "prod-vault-id" }
  }

  assert {
    condition     = output.vault_ids == { tack = { qa = "qa-vault-id", prod = "prod-vault-id" } }
    error_message = "Vault ID outputs must preserve the service and environment association."
  }
}

run "reject_invalid_service" {
  command = plan

  variables {
    services = { "tack/prod" = ["qa"] }
  }

  expect_failures = [var.services]
}

run "reject_invalid_environment" {
  command = plan

  variables {
    services = { tack = ["qa/prod"] }
  }

  expect_failures = [var.services]
}

run "reject_empty_environments" {
  command = plan

  variables {
    services = { tack = [] }
  }

  expect_failures = [var.services]
}

run "reject_null_environments" {
  command = plan

  variables {
    services = { tack = null }
  }

  expect_failures = [var.services]
}

run "reject_null_environment_name" {
  command = plan

  variables {
    services = { tack = [null] }
  }

  expect_failures = [var.services]
}
