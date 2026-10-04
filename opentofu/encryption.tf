terraform {
  # OpenTofu encrypts the state file in R2 and every saved plan file with a key
  # derived from the vault key vault_tofu_state_passphrase. OpenTofu stores the
  # names "state" and "migrate" inside the encrypted data, so renaming either
  # block makes the existing state unreadable.
  encryption {
    # The fallback reads the state that existed before encryption. The first
    # apply rewrites the state encrypted.
    method "unencrypted" "migrate" {}

    key_provider "pbkdf2" "state" {
      passphrase = var.vault_tofu_state_passphrase
    }

    method "aes_gcm" "state" {
      keys = key_provider.pbkdf2.state
    }

    state {
      method = method.aes_gcm.state

      fallback {
        method = method.unencrypted.migrate
      }
    }

    plan {
      method = method.aes_gcm.state

      fallback {
        method = method.unencrypted.migrate
      }
    }
  }
}
