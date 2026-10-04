terraform {
  # OpenTofu encrypts the state file in R2 and every saved plan file with a key
  # derived from the vault key vault_tofu_state_passphrase. OpenTofu stores the
  # name "state" inside the encrypted data. Renaming the key provider or the
  # method makes the existing state unreadable.
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
