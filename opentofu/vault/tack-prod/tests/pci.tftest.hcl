mock_provider "proxmox" {}
mock_provider "pveguest" {}

variables {
  vault_proxmox_token_secret        = "test"
  vault_seaweedfs_s3_access_key     = "test"
  vault_seaweedfs_s3_secret_key     = "test"
  vault_smtp2go_api_key             = "test"
  vault_tack_ghcr_pull_token        = "test"
  vault_tack_k3s_audit_operator_dsn = "test"
  vault_tack_k3s_database_url       = "test"
  vault_tack_k3s_fdb_bootstrap      = "test"
  vault_tack_k3s_server_token       = "test"
  vault_tack_search_ca              = "test"
  vault_tack_search_cursor_key      = "test"
  vault_tack_search_password        = "test"
  vault_tofu_state_passphrase       = "test-only-state-passphrase"
}

run "reject_secondary_vf_missing_from_pci_inventory" {
  command = plan

  plan_options {
    target = [proxmox_hardware_mapping_pci.tack_secondary]
  }

  override_data {
    target = data.pveguest_host_sriov.guests
    values = {
      actual_vfs = { "0" = {
        driver       = "vfio-pci"
        ifname       = null
        in_container = false
        mac          = "02:00:00:00:00:01"
        pci          = "0000:02:02.0"
      } }
    }
  }

  override_data {
    target = data.pveguest_host_sriov.guests_secondary
    values = {
      actual_vfs = { "0" = {
        driver       = "vfio-pci"
        ifname       = null
        in_container = false
        mac          = "02:00:00:00:00:02"
        pci          = "0000:02:02.1"
      } }
    }
  }

  override_data {
    target = data.proxmox_hardware_pci.tack
    values = {
      devices = [{
        class                 = "0x020000"
        device                = "0x154c"
        device_name           = "Ethernet VF"
        id                    = "0000:02:02.0"
        iommu_group           = 31
        mdev                  = false
        subsystem_device      = "0x0000"
        subsystem_device_name = ""
        subsystem_vendor      = "0x8086"
        subsystem_vendor_name = ""
        vendor                = "0x8086"
        vendor_name           = "Intel"
      }]
    }
  }

  expect_failures = [data.pveguest_host_sriov.guests_secondary]
}
