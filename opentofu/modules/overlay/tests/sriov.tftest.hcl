mock_provider "http" {
  mock_data "http" {
    defaults = {
      response_body = "test overlay contents"
      status_code   = 200
    }
  }
}

mock_provider "remote" {}

run "disable_sriov" {
  command = plan

  variables {
    ssh_host        = "proxmox.example"
    node_name       = "node"
    commit          = "0000000000000000000000000000000000000000"
    guest_api       = false
    automation_user = "automation@pam"
    acme_account    = "test"
    acme_plugin     = "test"
    sriov           = false
  }

  assert {
    condition = alltrue([
      for role, path in { ScopedSRIOV = "/", ScopedPCIMapping = "/mapping/pci" } :
      anytrue([
        for command in local.revoke_commands :
        strcontains(command, "pveum acl delete ${path} --users automation@pam --roles ${role}; pveum role delete ${role};")
      ])
    ])
    error_message = "Disabling SR-IOV must delete the automation user's ACL for each role before deleting the role."
  }
}
