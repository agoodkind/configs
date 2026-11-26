data "pveguest_host_sriov" "guests" {
  node   = module.vault_network.node.key
  ifname = module.vault_network.guest_pf
}

data "pveguest_host_sriov" "guests_secondary" {
  node   = module.vault_network.node.key
  ifname = module.vault_network.guest_pf_secondary

  lifecycle {
    postcondition {
      condition = length([
        for device in data.proxmox_hardware_pci.tack.devices : device
        if device.id == try(self.actual_vfs[tostring(local.tack_vf.index)].pci, null) && device.iommu_group >= 0
      ]) == 1
      error_message = "The secondary VF for Tack VM 124 at PCI address ${coalesce(try(self.actual_vfs[tostring(local.tack_vf.index)].pci, null), "unknown")} must match exactly one Vault PCI device with a valid IOMMU group."
    }
  }
}

locals {
  tack_vf          = module.vault_network.guest_networks.tack_k3s
  tack_pci_address = data.pveguest_host_sriov.guests.actual_vfs[tostring(local.tack_vf.index)].pci
}

data "proxmox_hardware_pci" "tack" {
  node_name           = module.vault_network.node.name
  pci_class_blacklist = []

  lifecycle {
    postcondition {
      condition     = length([for device in self.devices : device if device.id == local.tack_pci_address && device.iommu_group >= 0]) == 1
      error_message = "The assigned VM VF must match one PCI device with an IOMMU group."
    }
  }
}

locals {
  tack_pci = one([for device in data.proxmox_hardware_pci.tack.devices : device if device.id == local.tack_pci_address])
  tack_iommu_peers = [for device in data.proxmox_hardware_pci.tack.devices : device.id
    if device.iommu_group == local.tack_pci.iommu_group && device.id != local.tack_pci_address
  ]
}

resource "proxmox_hardware_mapping_pci" "tack" {
  name = "vault-tack-k3s"

  lifecycle {
    precondition {
      condition     = length(local.tack_iommu_peers) == 0
      error_message = "VM 124 VF ${local.tack_pci_address} must be the only PCI device in IOMMU group ${local.tack_pci.iommu_group}. Other devices: ${join(", ", local.tack_iommu_peers)}."
    }
  }

  map = [{
    node         = module.vault_network.node.name
    path         = local.tack_pci.id
    id           = "${trimprefix(local.tack_pci.vendor, "0x")}:${trimprefix(local.tack_pci.device, "0x")}"
    subsystem_id = "${trimprefix(local.tack_pci.subsystem_vendor, "0x")}:${trimprefix(local.tack_pci.subsystem_device, "0x")}"
    iommu_group  = local.tack_pci.iommu_group
  }]
}

locals {
  tack_secondary_pci_address = data.pveguest_host_sriov.guests_secondary.actual_vfs[tostring(local.tack_vf.index)].pci
  tack_secondary_pci = one([
    for device in data.proxmox_hardware_pci.tack.devices : device
    if device.id == local.tack_secondary_pci_address && device.iommu_group >= 0
  ])
  tack_secondary_iommu_peers = [for device in data.proxmox_hardware_pci.tack.devices : device.id
    if device.iommu_group == local.tack_secondary_pci.iommu_group && device.id != local.tack_secondary_pci_address
  ]
}

resource "proxmox_hardware_mapping_pci" "tack_secondary" {
  name = "vault-tack-k3s-secondary"

  lifecycle {
    precondition {
      condition     = local.tack_secondary_pci_address != local.tack_pci_address
      error_message = "Assign different PCI addresses to the two VM 124 VFs."
    }
    precondition {
      condition     = length(local.tack_secondary_iommu_peers) == 0
      error_message = "VM 124 VF ${local.tack_secondary_pci_address} must be the only PCI device in IOMMU group ${local.tack_secondary_pci.iommu_group}. Other devices: ${join(", ", local.tack_secondary_iommu_peers)}."
    }
  }

  map = [{
    node         = module.vault_network.node.name
    path         = local.tack_secondary_pci.id
    id           = "${trimprefix(local.tack_secondary_pci.vendor, "0x")}:${trimprefix(local.tack_secondary_pci.device, "0x")}"
    subsystem_id = "${trimprefix(local.tack_secondary_pci.subsystem_vendor, "0x")}:${trimprefix(local.tack_secondary_pci.subsystem_device, "0x")}"
    iommu_group  = local.tack_secondary_pci.iommu_group
  }]
}
