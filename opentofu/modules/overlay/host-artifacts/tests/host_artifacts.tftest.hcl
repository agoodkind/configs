# Each run plans the module against the synthetic repository in
# tests/repository. The repository also contains remote/broken, a descriptor
# with invalid JSON for another host.
variables {
  repository_root = "tests/repository"
  node_name       = "lab"
}

run "sibling_directories_pass" {
  command = plan

  variables {
    workspaces = ["lab/alpha", "lab/alpha_sibling"]
  }

  assert {
    condition     = keys(output.policies) == ["lab/alpha", "lab/alpha_sibling"]
    error_message = "the plan must contain one policy for each selected workspace"
  }

  assert {
    condition     = jsondecode(output.policies["lab/alpha_sibling"]).parent_directories == [{ path = "/etc/systemd/system" }]
    error_message = "an inspect-only parent without metadata must keep only its path"
  }
}

run "broken_descriptor_of_another_host_does_not_change_plan" {
  command = plan

  variables {
    workspaces = ["lab/alpha"]
  }

  assert {
    condition = output.policies["lab/alpha"] == trimspace(<<-EOT
      {"configuration":{"max_archive_bytes":8388608,"max_bundle_bytes":1048576,"repository":"agoodkind/configs","source_ref":"refs/heads/main","workflow_uri":"https://github.com/agoodkind/configs/.github/workflows/ci.yml@refs/heads/main"},"directories":[{"group":"root","mode":"0755","owner":"root","path":"/etc/alpha"}],"environment":"testbed","files":[{"group":"root","mode":"0600","owner":"root","path":"/etc/alpha/config.toml"},{"group":"root","mode":"0644","owner":"root","path":"/etc/systemd/system/alpha.service"}],"node":"lab","parent_directories":[{"group":"root","mode":"0755","owner":"root","path":"/etc/systemd/system"}],"secret_fields":[{"consumer_units":["alpha.service"],"group":"root","max_length":64,"min_length":16,"mode":"0600","name":"ALPHA_TOKEN","owner":"root","pattern":"^[A-Za-z0-9]+$","storage_path":"/etc/alpha/secrets.env"}],"workspace":"lab/alpha"}
    EOT
    )
    error_message = "the policy of lab/alpha must equal the expected document"
  }
}

run "empty_selection_has_no_policy" {
  command = plan

  variables {
    workspaces = []
  }

  assert {
    condition     = length(output.policies) == 0
    error_message = "an empty selection cannot produce a policy"
  }
}

run "shared_owned_directory_fails" {
  command = plan

  variables {
    workspaces = ["lab/empty-a", "lab/empty-b"]
  }

  expect_failures = [output.policies]
}

run "owned_and_inspected_directory_fails" {
  command = plan

  variables {
    workspaces = ["lab/inspect-a", "lab/inspect-b"]
  }

  expect_failures = [output.policies]
}

run "owned_directory_contains_file_fails" {
  command = plan

  variables {
    workspaces = ["lab/outer", "lab/inner-file"]
  }

  expect_failures = [output.policies]
}

run "owned_directory_contains_secret_storage_path_fails" {
  command = plan

  variables {
    workspaces = ["lab/outer", "lab/inner-secret"]
  }

  expect_failures = [output.policies]
}

run "owned_directory_contains_owned_directory_fails" {
  command = plan

  variables {
    workspaces = ["lab/outer", "lab/inner-directory"]
  }

  expect_failures = [output.policies]
}

run "owned_directory_contains_inspected_parent_fails" {
  command = plan

  variables {
    workspaces = ["lab/outer", "lab/inner-parent"]
  }

  expect_failures = [output.policies]
}

run "owned_directory_equals_file_fails" {
  command = plan

  variables {
    workspaces = ["lab/outer", "lab/equal-file"]
  }

  expect_failures = [output.policies]
}

run "shared_file_destination_fails" {
  command = plan

  variables {
    workspaces = ["lab/file-a", "lab/file-b"]
  }

  expect_failures = [output.policies]
}

run "shared_secret_storage_path_fails" {
  command = plan

  variables {
    workspaces = ["lab/secret-a", "lab/secret-b"]
  }

  expect_failures = [output.policies]
}

run "path_inside_etc_pve_overlay_fails" {
  command = plan

  variables {
    workspaces = ["lab/protected-etc"]
  }

  expect_failures = [output.policies]
}

run "path_equal_to_var_lib_pve_overlay_fails" {
  command = plan

  variables {
    workspaces = ["lab/protected-var"]
  }

  expect_failures = [output.policies]
}

run "relative_path_fails" {
  command = plan

  variables {
    workspaces = ["lab/relative"]
  }

  expect_failures = [output.policies]
}

run "dot_dot_component_fails" {
  command = plan

  variables {
    workspaces = ["lab/dot-dot"]
  }

  expect_failures = [output.policies]
}

run "trailing_slash_fails" {
  command = plan

  variables {
    workspaces = ["lab/trailing-slash"]
  }

  expect_failures = [output.policies]
}

run "descriptor_of_another_node_fails" {
  command = plan

  variables {
    workspaces = ["lab/other-node"]
  }

  expect_failures = [output.policies]
}

run "sibling_programs_under_inspected_parent_pass" {
  command = plan

  variables {
    workspaces = ["lab/prog-a", "lab/prog-b"]
  }

  assert {
    condition = jsondecode(output.policies["lab/prog-a"]).programs == [{
      name              = "tool-a"
      repository        = "agoodkind/tool-a"
      workflow_uri      = "https://github.com/agoodkind/go-makefile/.github/workflows/_package.yml@refs/heads/main"
      source_ref        = "refs/heads/main"
      path              = "/usr/local/bin/tool-a"
      mode              = "0755"
      owner             = "root"
      group             = "root"
      max_archive_bytes = 67108864
      max_bundle_bytes  = 1048576
    }]
    error_message = "the policy must list the program with the fixed workflow, source ref, and size limits"
  }

  assert {
    condition     = jsondecode(output.policies["lab/prog-b"]).parent_directories == [{ path = "/usr/local/bin" }]
    error_message = "a program path must not make /usr/local/bin an owned directory"
  }
}

run "target_without_programs_next_to_targets_with_programs_passes" {
  command = plan

  variables {
    workspaces = ["lab/alpha", "lab/prog-a", "lab/prog-b"]
  }

  assert {
    condition     = !contains(keys(jsondecode(output.policies["lab/alpha"])), "programs")
    error_message = "the policy of lab/alpha must not include a programs key"
  }

  assert {
    condition = alltrue([
      for workspace in ["lab/prog-a", "lab/prog-b"] :
      length(jsondecode(output.policies[workspace]).programs) == 1
    ])
    error_message = "the policies of lab/prog-a and lab/prog-b must each list exactly one program"
  }
}

run "shared_program_path_fails" {
  command = plan

  variables {
    workspaces = ["lab/prog-shared-a", "lab/prog-shared-b"]
  }

  expect_failures = [output.policies]
}

run "program_path_equals_secret_storage_path_of_other_target_fails" {
  command = plan

  variables {
    workspaces = ["lab/secret-a", "lab/prog-secret-path"]
  }

  expect_failures = [output.policies]
}

run "program_path_inside_owned_directory_of_other_target_fails" {
  command = plan

  variables {
    workspaces = ["lab/outer", "lab/prog-inner"]
  }

  expect_failures = [output.policies]
}

run "program_path_equals_file_of_same_target_fails" {
  command = plan

  variables {
    workspaces = ["lab/prog-file-collision"]
  }

  expect_failures = [output.policies]
}

run "program_path_equals_secret_storage_path_of_same_target_fails" {
  command = plan

  variables {
    workspaces = ["lab/prog-secret-collision"]
  }

  expect_failures = [output.policies]
}

run "two_programs_with_one_path_in_one_target_fail" {
  command = plan

  variables {
    workspaces = ["lab/prog-twice"]
  }

  expect_failures = [output.policies]
}

run "workspace_without_descriptor_fails" {
  command = plan

  variables {
    workspaces = ["lab/missing"]
  }

  expect_failures = [output.policies]
}
