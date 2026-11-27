mock_provider "proxmox" {
  mock_resource "proxmox_virtual_environment_file" {
    defaults = { id = "local:snippets/worker-cloud-init.yaml" }
  }
}

run "boot_worker_from_shared_image_with_bonded_vfs" {
  command = plan

  variables {
    guest = {
      node              = "vault"
      node_name         = "vault"
      vmid              = 127
      hostname          = "worker.example"
      service_key       = "worker"
      mac_address       = "02:00:00:00:00:01"
      dns_servers       = ["2001:db8::53"]
      datastore_id      = "local-lvm"
      image_datastore   = "local"
      snippet_datastore = "local"
    }
    cloud_image           = null
    shared_cloud_image_id = "local:import/debian.qcow2"
    network_data_file_id  = "local:snippets/worker-network.yaml"
    pci_mapping           = "worker-primary"
    secondary_pci_mapping = "worker-secondary"
    vm_capacity           = { cores = 2, memory_mib = 4096, disk_gib = 20 }
    tags                  = []
    initialization        = { public_keys = [], packages = ["qemu-guest-agent"], filename_prefix = "worker" }
  }

  assert {
    condition = (
      length(proxmox_download_file.cloud_image) == 0 &&
      one(proxmox_virtual_environment_vm.server.disk).import_from == var.shared_cloud_image_id
    )
    error_message = "The worker must import the existing image without downloading another copy."
  }

  assert {
    condition = (
      one(proxmox_virtual_environment_vm.server.initialization).network_data_file_id == var.network_data_file_id &&
      length(one(proxmox_virtual_environment_vm.server.initialization).ip_config) == 0 &&
      length(proxmox_virtual_environment_vm.server.network_device) == 0 &&
      toset([for device in proxmox_virtual_environment_vm.server.hostpci : device.mapping]) == toset([var.pci_mapping, var.secondary_pci_mapping])
    )
    error_message = "Initialize networking from the cloud-init snippet with both PCI devices attached."
  }
}
