data "pveguest_host_sriov" "guests" {
  node   = module.vault_network.node.key
  ifname = module.vault_network.guest_pf
}

locals {
  tack_vf          = module.vault_network.guest_networks.tack_k3s
  tack_pci_address = data.pveguest_host_sriov.guests.actual_vfs[tostring(local.tack_vf.index)].pci
}

data "proxmox_hardware_pci" "tack" {
  node_name = module.vault_network.node.name
  filters   = { id = local.tack_pci_address }

  lifecycle {
    postcondition {
      condition     = length([for device in self.devices : device if device.id == local.tack_pci_address && device.iommu_group >= 0]) == 1
      error_message = "The assigned VM VF must match one PCI device with an IOMMU group."
    }
  }
}

locals {
  tack_pci = one([for device in data.proxmox_hardware_pci.tack.devices : device if device.id == local.tack_pci_address])
}

resource "proxmox_hardware_mapping_pci" "tack" {
  name = "vault-tack-k3s"
  map = [{
    node         = module.vault_network.node.name
    path         = local.tack_pci.id
    id           = "${trimprefix(local.tack_pci.vendor, "0x")}:${trimprefix(local.tack_pci.device, "0x")}"
    subsystem_id = "${trimprefix(local.tack_pci.subsystem_vendor, "0x")}:${trimprefix(local.tack_pci.subsystem_device, "0x")}"
    iommu_group  = local.tack_pci.iommu_group
  }]
}
