mock_provider "http" {}
mock_provider "proxmox" {}

mock_provider "proxmox" {
  alias = "vault_root"
}

mock_provider "proxmox" {
  alias = "suburban"
}

mock_provider "proxmox" {
  alias = "suburban_root"
}

mock_provider "proxmox" {
  alias = "poweredge_root"
}

variables {
  vault_proxmox_token_secret              = "test"
  vault_proxmox_root_password             = "test"
  proxmox_root_otp                        = "000000"
  vault_proxmox_acme_cloudflare_token     = "test"
  vault_suburban_testbed_pve_token_secret = "test"
  vault_suburban_proxmox_root_password    = "test"
  vault_suburban_acme_cloudflare_token    = "test"
  vault_poweredge_proxmox_root_password   = "test"
  vault_poweredge_acme_cloudflare_token   = "test"
  vault_tofu_state_passphrase             = "test-fixture-phrase-0123456789"
}

override_module {
  target = module.suburban
}

override_module {
  target = module.vault
}

override_module {
  target = module.vault_root_login
  outputs = {
    auth_ticket           = "test"
    csrf_prevention_token = "test"
  }
}

run "accepts_github_ssh_keys" {
  command = plan

  override_data {
    target = data.http.github_ssh_keys
    values = {
      response_body = "ssh-ed25519 AAAAGITHUB github-key\n"
      status_code   = 200
    }
  }
}

run "rejects_non_key_response" {
  command = plan

  override_data {
    target = data.http.github_ssh_keys
    values = {
      response_body = "<html>rate limited</html>"
      status_code   = 200
    }
  }

  expect_failures = [data.http.github_ssh_keys]
}

run "rejects_non_success_response" {
  command = plan

  override_data {
    target = data.http.github_ssh_keys
    values = {
      response_body = "ssh-ed25519 AAAANOTFOUND error-page"
      status_code   = 404
    }
  }

  expect_failures = [data.http.github_ssh_keys]
}
