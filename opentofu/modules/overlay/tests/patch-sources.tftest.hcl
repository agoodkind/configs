mock_provider "http" {
  mock_data "http" {
    defaults = {
      response_body = "other patch\n"
      status_code   = 200
    }
  }
}

mock_provider "remote" {}

variables {
  ssh_host        = "proxmox.example"
  node_name       = "node"
  commit          = "0000000000000000000000000000000000000000"
  guest_api       = false
  automation_user = "automation@pam"
  acme_account    = "test"
  acme_plugin     = "test"
}

run "install_default_patch_series" {
  command = plan

  override_data {
    target = data.http.files["patches/pve-manager-20-kernel-modules.patch"]
    values = {
      response_body = "kernel modules\n"
      status_code   = 200
    }
  }

  override_data {
    target = data.http.files["patches/pve-manager-30-host-artifacts.patch"]
    values = {
      response_body = "host artifacts\n"
      status_code   = 200
    }
  }

  override_data {
    target = data.http.files["patches/pve-manager-10-acme.patch"]
    values = {
      response_body = "certificates\n"
      status_code   = 200
    }
  }

  assert {
    condition = remote_file.files["patches/pve-manager.patch"].content == "kernel modules\nhost artifacts\nother patch\nother patch\nother patch\nother patch\n" && (
      remote_file.files["patches/pve-manager-acme.patch"].content == "certificates\n"
    )
    error_message = "Install the manager patches in feature order and install the certificate patch in its own file."
  }

  assert {
    condition = tomap({ for key, file in remote_file.files : key => file.path }) == tomap({
      "pve-overlay"                      = "/usr/local/sbin/pve-overlay"
      "patches/pve-access-control.patch" = "/usr/local/share/pve-overlay-pve-access-control.patch"
      "patches/pve-container.patch"      = "/usr/local/share/pve-overlay-pve-container.patch"
      "patches/pve-http-server.patch"    = "/usr/local/share/pve-overlay-pve-http-server.patch"
      "patches/pve-manager-acme.patch"   = "/usr/local/share/pve-overlay-pve-manager-acme.patch"
      "patches/pve-manager.patch"        = "/usr/local/share/pve-overlay-pve-manager.patch"
      "patches/qemu-server.patch"        = "/usr/local/share/pve-overlay-qemu-server.patch"
      }) && alltrue([
      for key, file in remote_file.files : file.permissions == (key == "pve-overlay" ? "0755" : "0644")
    ])
    error_message = "Preserve the seven managed file keys, installed paths, and permissions."
  }
}

run "override_patch_sources" {
  command = plan

  variables {
    base_patch_sources = {
      "patches/pve-container.patch" = ["patches/pve-container.patch"]
      "patches/extra.patch"         = ["patches/pve-container.patch", "patches/compat/extra.patch"]
    }
  }

  override_data {
    target = data.http.files["patches/pve-container.patch"]
    values = {
      response_body = "legacy container patch\n"
      status_code   = 200
    }
  }

  override_data {
    target = data.http.files["patches/compat/extra.patch"]
    values = {
      response_body = "extra patch\n"
      status_code   = 200
    }
  }

  assert {
    condition = remote_file.files["patches/pve-container.patch"].content == "legacy container patch\n" && (
      remote_file.files["patches/extra.patch"].content == "legacy container patch\nextra patch\n"
    )
    error_message = "Replace the container series with the singleton source and concatenate the extra sources in their declared order."
  }
}

run "reject_empty_patch_sources" {
  command = plan

  variables {
    base_patch_sources = {
      "patches/pve-container.patch" = []
    }
  }

  expect_failures = [var.base_patch_sources]
}
