moved {
  from = proxmox_virtual_environment_container.tack_qa
  to   = proxmox_virtual_environment_container.tack_qa_suburban
}

moved {
  from = proxmox_virtual_environment_container.seaweedfs
  to   = proxmox_virtual_environment_container.seaweedfs_suburban
}

moved {
  from = proxmox_virtual_environment_container.dns64
  to   = proxmox_virtual_environment_container.dns64_suburban
}

moved {
  from = proxmox_virtual_environment_container.mwan_failover_test
  to   = proxmox_virtual_environment_container.mwan_failover_suburban
}

moved {
  from = proxmox_virtual_environment_container.isp_webpass
  to   = proxmox_virtual_environment_container.isp_webpass_suburban
}

moved {
  from = proxmox_virtual_environment_container.isp_att
  to   = proxmox_virtual_environment_container.isp_att_suburban
}

moved {
  from = proxmox_virtual_environment_container.isp_mbrains
  to   = proxmox_virtual_environment_container.isp_mbrains_suburban
}

moved {
  from = proxmox_virtual_environment_vm.test_mwan
  to   = proxmox_virtual_environment_vm.mwan_suburban
}

moved {
  from = proxmox_virtual_environment_vm.opnsense_test
  to   = proxmox_virtual_environment_vm.opnsense_suburban
}

moved {
  from = proxmox_network_linux_bridge.vm_management
  to   = proxmox_network_linux_bridge.vm_management_suburban
}

moved {
  from = proxmox_network_linux_bridge.mwan_internal
  to   = proxmox_network_linux_bridge.mwan_suburban
}

moved {
  from = proxmox_network_linux_bridge.isp_webpass
  to   = proxmox_network_linux_bridge.isp_webpass_suburban
}

moved {
  from = proxmox_network_linux_bridge.isp_att
  to   = proxmox_network_linux_bridge.isp_att_suburban
}

moved {
  from = proxmox_network_linux_bridge.isp_mbrains
  to   = proxmox_network_linux_bridge.isp_mbrains_suburban
}

moved {
  from = proxmox_network_linux_bridge.trunk
  to   = proxmox_network_linux_bridge.trunk_suburban
}

moved {
  from = proxmox_network_linux_vlan.trunk_vlan_100
  to   = proxmox_network_linux_vlan.trunk_vlan_100_suburban
}
