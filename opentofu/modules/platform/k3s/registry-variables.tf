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
