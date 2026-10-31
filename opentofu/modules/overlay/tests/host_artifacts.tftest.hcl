# The mock http provider replaces the download of the overlay files from
# GitHub. The remote provider plans each file without an SSH connection.
mock_provider "http" {
  mock_data "http" {
    defaults = {
      status_code   = 200
      response_body = "overlay file"
    }
  }
}

variables {
  ssh_host        = "2001:db8::1"
  node_name       = "lab"
  commit          = "0000000000000000000000000000000000000000"
  guest_api       = false
  automation_user = "automation@pve"
  acme_account    = "default"
  acme_plugin     = "cf"
}

run "policies_use_the_selector_digest" {
  command = plan

  variables {
    host_artifact_policies = {
      "lab/alpha" = "{\"workspace\":\"lab/alpha\"}"
    }
  }

  assert {
    condition     = remote_file.host_artifact_policies["lab/alpha"].path == "/etc/pve-overlay/host-artifacts/targets/${sha256("lab/alpha")}.json"
    error_message = "the policy file name must be the SHA-256 of the workspace selector"
  }

  assert {
    condition     = remote_file.host_artifact_policies["lab/alpha"].content == "{\"workspace\":\"lab/alpha\"}\n"
    error_message = "the policy file must contain the policy document and a newline"
  }

  assert {
    condition     = remote_file.host_artifacts_trusted_root[0].content == file("host-artifacts-trusted-root.jsonl")
    error_message = "the trusted root file must contain the committed trusted root"
  }

  assert {
    condition     = contains(local.grant_commands, "pveum acl modify / --users automation@pve --roles ScopedHostArtifacts --propagate 1")
    error_message = "the module must grant ScopedHostArtifacts on / with propagation"
  }

  assert {
    condition     = local.roles.ScopedHostArtifacts.privileges == ["Sys.HostArtifacts.Audit", "Sys.HostArtifacts.Modify", "Sys.HostSecrets.Modify"]
    error_message = "ScopedHostArtifacts must contain the three host artifact privileges"
  }

  assert {
    condition     = !anytrue([for command in local.grant_commands : strcontains(command, "/nodes/")])
    error_message = "the module does not grant a role on a /nodes path"
  }
}

run "empty_policies_write_nothing" {
  command = plan

  assert {
    condition     = length(remote_file.host_artifact_policies) == 0 && length(remote_file.host_artifacts_trusted_root) == 0 && length(terraform_data.host_artifacts_directory) == 0
    error_message = "an empty policy map must not write a host artifact file or directory"
  }

  assert {
    condition     = !contains(keys(local.roles), "ScopedHostArtifacts")
    error_message = "the module does not define ScopedHostArtifacts when the policy map is empty"
  }
}
