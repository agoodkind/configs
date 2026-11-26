mock_provider "proxmox" {
  mock_resource "proxmox_download_file" {
    defaults = { id = "local:import/debian.qcow2" }
  }

  mock_resource "proxmox_virtual_environment_file" {
    defaults = { id = "local:snippets/test-124-cloud-init.yaml" }
  }
}

variables {
  guest = {
    node              = "test"
    node_name         = "test"
    vmid              = 124
    hostname          = "vm.example.com"
    service_key       = "test"
    mac_address       = "bc:24:11:a3:52:7c"
    ipv6_cidr         = "2001:db8::124/64"
    dns_servers       = ["2001:db8::53"]
    datastore_id      = "local-lvm"
    image_datastore   = "local"
    snippet_datastore = "local"
  }
  cloud_image = {
    url       = "https://example.com/debian.qcow2"
    file_name = "debian.qcow2"
    sha512    = sha512("test cloud image")
  }
  vm_capacity = {
    cores      = 2
    memory_mib = 2048
    disk_gib   = 8
  }
  initialization = {
    public_keys     = []
    packages        = []
    filename_prefix = "test"
  }
  tags = []
}

run "bridge" {
  command = plan

  variables {
    guest = {
      node              = "test"
      node_name         = "test"
      vmid              = 124
      hostname          = "vm.example.com"
      service_key       = "test"
      bridge            = "vmbr0"
      mac_address       = "bc:24:11:a3:52:7c"
      ipv6_cidr         = "2001:db8::124/64"
      dns_servers       = ["2001:db8::53"]
      datastore_id      = "local-lvm"
      image_datastore   = "local"
      snippet_datastore = "local"
    }
  }

  assert {
    condition = (
      one(proxmox_virtual_environment_vm.server.network_device).bridge == "vmbr0" &&
      one(proxmox_virtual_environment_vm.server.network_device).mac_address == "bc:24:11:a3:52:7c" &&
      length(proxmox_virtual_environment_vm.server.hostpci) == 0 &&
      length(one(proxmox_virtual_environment_vm.server.initialization).ip_config) == 1
    )
    error_message = "A bridged VM must keep its network device and cloud-init IP configuration."
  }
}

run "single_mapping" {
  command = plan

  variables {
    pci_mapping = "primary"
  }

  assert {
    condition = (
      length(proxmox_virtual_environment_vm.server.network_device) == 0 &&
      length(one(proxmox_virtual_environment_vm.server.initialization).ip_config) == 0 &&
      { for attachment in proxmox_virtual_environment_vm.server.hostpci : attachment.device => attachment.mapping } == { hostpci0 = "primary" }
    )
    error_message = "A VM with one PCI mapping must attach it to hostpci0 without a bridged device or cloud-init IP configuration."
  }
}

run "two_mappings" {
  command = plan

  variables {
    pci_mapping           = "primary"
    secondary_pci_mapping = "secondary"
  }

  assert {
    condition = (
      length(proxmox_virtual_environment_vm.server.network_device) == 0 &&
      { for attachment in proxmox_virtual_environment_vm.server.hostpci : attachment.device => attachment.mapping } == {
        hostpci0 = "primary"
        hostpci1 = "secondary"
      }
    )
    error_message = "The two PCI mappings must attach to hostpci0 and hostpci1 in order."
  }
}

run "secondary_without_primary" {
  command = plan

  variables {
    secondary_pci_mapping = "secondary"
  }

  expect_failures = [var.secondary_pci_mapping]
}

run "duplicate_mappings" {
  command = plan

  variables {
    pci_mapping           = "primary"
    secondary_pci_mapping = "primary"
  }

  expect_failures = [var.secondary_pci_mapping]
}
