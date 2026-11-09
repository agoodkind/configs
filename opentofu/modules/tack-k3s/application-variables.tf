variable "application" {
  description = "Immutable application image, runtime environment, database host aliases, and public ledger CA."
  type = object({
    image         = string
    environment   = map(string)
    ledger_hosts  = map(string)
    ledger_ca_pem = string
  })

  validation {
    condition = (
      can(regex("^ghcr.io/agoodkind/tack-server@sha256:[a-f0-9]{64}$", var.application.image)) &&
      length(var.application.ledger_hosts) > 0 &&
      strcontains(var.application.ledger_ca_pem, "-----BEGIN CERTIFICATE-----") &&
      alltrue([for key in ["ENV", "AUDIT_KAFKA_BROKERS", "OPENSEARCH_ENDPOINT", "OPENSEARCH_USERNAME"] :
        length(trimspace(lookup(var.application.environment, key, ""))) > 0
      ])
    )
    error_message = "Supply an immutable Tack image, ledger hosts and CA, and the required application environment values."
  }
}

variable "application_secrets" {
  description = "Protected application credentials, search CA, and initial FoundationDB descriptor."
  type = object({
    database_url       = string
    audit_operator_dsn = string
    search_password    = string
    search_cursor_key  = string
    search_ca          = string
    fdb_bootstrap      = string
  })
  sensitive = true
  ephemeral = true

  validation {
    condition = alltrue([
      for value in values(var.application_secrets) : length(trimspace(value)) > 0
    ])
    error_message = "Supply every protected application credential, the search CA, and the FoundationDB bootstrap descriptor."
  }
}

variable "application_secret_versions" {
  description = "Increment each version when its protected application file changes."
  type = object({
    database_url       = number
    audit_operator_dsn = number
    search_password    = number
    search_cursor_key  = number
    search_ca          = number
    fdb_bootstrap      = number
  })

  validation {
    condition = alltrue([
      for version in values(var.application_secret_versions) : version >= 1 && floor(version) == version
    ])
    error_message = "Set every application secret version to a positive integer."
  }
}

variable "registry_credentials" {
  description = "Protected GHCR pull credential for the native k3s registry configuration."
  type = object({
    username = string
    token    = string
  })
  sensitive = true
  ephemeral = true

  validation {
    condition     = length(trimspace(var.registry_credentials.username)) > 0 && length(trimspace(var.registry_credentials.token)) > 0
    error_message = "Supply the GHCR username and pull token from the protected credential source."
  }
}

variable "registry_credentials_version" {
  description = "Increment this version when the protected GHCR credential changes."
  type        = number

  validation {
    condition     = var.registry_credentials_version >= 1 && floor(var.registry_credentials_version) == var.registry_credentials_version
    error_message = "Set registry_credentials_version to a positive integer."
  }
}
