data "pveguest_host_sriov" "guests" {
  node   = module.vault_network.node.key
  ifname = module.vault_network.guest_pf
}

data "pveguest_host_sriov" "guests_secondary" {
  node   = module.vault_network.node.key
  ifname = module.vault_network.guest_pf_secondary
}

data "proxmox_hardware_pci" "tack" {
  node_name           = module.vault_network.node.name
  pci_class_blacklist = []
}

locals {
  tack_vf = module.vault_network.guest_networks.tack_k3s
  vm_vfs = { for key in setunion(["tack_k3s"], keys(module.configuration.k3s_workers)) :
    key => module.vault_network.guest_networks[key].index
  }
  pci_addresses = merge([
    for key, vf in local.vm_vfs : {
      (key)                = data.pveguest_host_sriov.guests.actual_vfs[tostring(vf)].pci
      ("${key}-secondary") = data.pveguest_host_sriov.guests_secondary.actual_vfs[tostring(vf)].pci
    }
  ]...)
  pci_devices = { for key, address in local.pci_addresses : key =>
    one([for device in data.proxmox_hardware_pci.tack.devices : device if device.id == address])
  }
}

resource "proxmox_hardware_mapping_pci" "guest" {
  for_each = local.pci_devices
  name     = "vault-${replace(each.key, "_", "-")}"

  lifecycle {
    precondition {
      condition = try(each.value.iommu_group >= 0 && length([
        for device in data.proxmox_hardware_pci.tack.devices : device.id
        if device.iommu_group == each.value.iommu_group
      ]) == 1, false)
      error_message = "Assign ${each.key} a Vault VF with its own IOMMU group."
    }
    precondition {
      condition     = length(distinct(values(local.pci_addresses))) == length(local.pci_addresses)
      error_message = "Assign distinct PCI devices to every VM network interface."
    }
  }

  map = [{
    node         = module.vault_network.node.name
    path         = each.value.id
    id           = "${trimprefix(each.value.vendor, "0x")}:${trimprefix(each.value.device, "0x")}"
    subsystem_id = "${trimprefix(each.value.subsystem_vendor, "0x")}:${trimprefix(each.value.subsystem_device, "0x")}"
    iommu_group  = each.value.iommu_group
  }]
}

moved {
  from = proxmox_hardware_mapping_pci.tack
  to   = proxmox_hardware_mapping_pci.guest["tack_k3s"]
}

moved {
  from = proxmox_hardware_mapping_pci.tack_secondary
  to   = proxmox_hardware_mapping_pci.guest["tack_k3s-secondary"]
}
