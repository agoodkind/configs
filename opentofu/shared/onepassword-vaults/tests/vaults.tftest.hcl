mock_provider "opadmin" {}

variables {
  vault_tofu_state_passphrase = "disposable-test-passphrase"
}

run "declare_tack_qa_and_prod" {
  command = plan

  assert {
    condition     = toset(keys(module.vaults.vault_ids)) == toset(["tack"])
    error_message = "The provisioning root must declare Tack vaults."
  }

  assert {
    condition     = toset(keys(module.vaults.vault_ids.tack)) == toset(["qa", "prod"])
    error_message = "Tack QA and production must have separate vaults."
  }
}
