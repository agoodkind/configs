mock_provider "http" {}
mock_provider "proxmox" {}

mock_provider "proxmox" {
  alias = "suburban"
}

variables {
  proxmox_api_token          = "test"
  suburban_proxmox_api_token = "test"
}

override_module {
  target = module.suburban
}

override_module {
  target = module.vault
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
