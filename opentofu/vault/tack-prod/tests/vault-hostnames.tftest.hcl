variables {
  guest_keys = ["tack"]
  service_mapping = {
    tack = {
      hostname      = "tack-prod-001.vault.goodkind.io"
      vmid          = 117
      ipv6          = "2001:db8::117"
      mail_username = "host-tack-home-goodkind-io"
      mail_from     = "tack@goodkind.io"
    }
    proxy = { ipv6 = "2001:db8::110" }
  }
  shared_vars = {
    smtp_host        = "mail.smtp2go.com"
    smtp_port        = 587
    smtp_from_domain = "goodkind.io"
  }
  proxy_vars = { sshpiper_upstream_pubkey = "ssh-ed25519 test" }
  timezone   = "America/Los_Angeles"
}

run "renamed_vault_guest" {
  command = plan

  module {
    source = "../../modules/guest-inputs"
  }

  assert {
    condition     = output.guests.tack.node == "vault"
    error_message = "The renamed guest must use the Vault provider node."
  }

  assert {
    condition     = output.mail_relays.tack.username == "host-tack-home-goodkind-io"
    error_message = "Renaming the guest must preserve its SMTP account."
  }

  assert {
    condition     = output.mail_relays.tack.from == "tack@goodkind.io"
    error_message = "Renaming the guest must preserve its mail sender."
  }

  assert {
    condition     = output.mail_relay_import_ids.tack == "vault/117/lxc/host-tack-home-goodkind-io/1"
    error_message = "Renaming the guest must preserve its mail resource identity."
  }
}
