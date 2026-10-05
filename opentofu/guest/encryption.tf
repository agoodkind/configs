variable "vault_tofu_state_passphrase" {
  description = "Passphrase that encrypts the OpenTofu state and plan files"
  type        = string
  sensitive   = true
}

terraform {
  # OpenTofu writes the key provider name and the method name "state" into the
  # encrypted state file. OpenTofu cannot decrypt that file after a rename of
  # either block.
  encryption {
    key_provider "pbkdf2" "state" {
      passphrase = var.vault_tofu_state_passphrase
    }

    method "aes_gcm" "state" {
      keys = key_provider.pbkdf2.state
    }

    state {
      method   = method.aes_gcm.state
      enforced = true
    }

    plan {
      method   = method.aes_gcm.state
      enforced = true
    }
  }
}
