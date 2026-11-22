variable "guest" {
  description = "Proxmox VM identity, network addresses, and disk storage."
  type = object({
    node              = string
    node_name         = string
    vmid              = number
    hostname          = string
    service_key       = string
    bridge            = optional(string)
    mac_address       = string
    ipv4_cidr         = optional(string)
    ipv4_gateway      = optional(string)
    ipv6_cidr         = string
    ipv6_gateway      = optional(string)
    dns_servers       = list(string)
    datastore_id      = string
    image_datastore   = string
    snippet_datastore = string
  })

  validation {
    condition = (
      (var.guest.ipv4_cidr == null || can(cidrnetmask(var.guest.ipv4_cidr))) &&
      can(cidrhost(var.guest.ipv6_cidr, 0)) && strcontains(var.guest.ipv6_cidr, ":") &&
      length(var.guest.dns_servers) > 0
    )
    error_message = "Set a valid IPv6 CIDR and at least one DNS server for the VM. The optional IPv4 CIDR must be valid."
  }
}

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

variable "application_replicas" {
  description = "Set the number of Tack application Pods. A single replica updates without a surge."
  type        = number
  validation {
    condition     = var.application_replicas >= 1 && floor(var.application_replicas) == var.application_replicas
    error_message = "Set application_replicas to a positive integer."
  }
}

variable "identity" {
  type = object({ namespace = string, name = string, port = number })
}
variable "application_strategy" {
  type = string
  validation {
    condition     = contains(["Recreate", "RollingUpdate"], var.application_strategy)
    error_message = "Set application_strategy to Recreate or RollingUpdate."
  }
}
