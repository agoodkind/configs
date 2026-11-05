terraform {
  backend "local" {
    path = "/var/lib/configsctl-acceptance/state.tfstate"
  }
}

variable "vault_installed_source_token" {
  type      = string
  sensitive = true
  ephemeral = true

  validation {
    condition     = sha256(var.vault_installed_source_token) == var.expected_token_sha256
    error_message = "The supplied token must match the expected SHA-256 digest."
  }
}

variable "expected_token_sha256" {
  type = string
}

module "host" {
  source = "./host"
}
