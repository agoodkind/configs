data "pveguest_host_sriov" "guests" {
  node   = local.guest.node
  ifname = module.configuration.network.pf
}

locals {
  address = data.pveguest_host_sriov.guests.actual_vfs[tostring(module.configuration.network.index)].pci
}

data "proxmox_hardware_pci" "terrakube" {
  node_name           = local.guest.node_name
  pci_class_blacklist = []

  lifecycle {
    postcondition {
      condition     = length([for device in self.devices : device if device.id == local.address && device.iommu_group >= 0]) == 1
      error_message = "The Terrakube VF must match one PCI device with an IOMMU group."
    }
  }
}

locals {
  device = one([for device in data.proxmox_hardware_pci.terrakube.devices : device if device.id == local.address])
  peers  = [for device in data.proxmox_hardware_pci.terrakube.devices : device.id if device.iommu_group == local.device.iommu_group && device.id != local.address]
}

resource "proxmox_hardware_mapping_pci" "terrakube" {
  name = "vault-terrakube"

  lifecycle {
    precondition {
      condition     = length(local.peers) == 0
      error_message = "The Terrakube VF must be the only device in its IOMMU group."
    }
  }

  map = [{
    node         = local.guest.node_name
    path         = local.device.id
    id           = "${trimprefix(local.device.vendor, "0x")}:${trimprefix(local.device.device, "0x")}"
    subsystem_id = "${trimprefix(local.device.subsystem_vendor, "0x")}:${trimprefix(local.device.subsystem_device, "0x")}"
    iommu_group  = local.device.iommu_group
  }]
}
