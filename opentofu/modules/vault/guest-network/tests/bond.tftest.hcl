mock_provider "pveguest" {}

variables {
  dns_servers = ["2001:db8::53"]
}

run "lxc_bond_preserves_network_identity" {
  command = plan

  variables {
    guest = {
      vmid             = 117
      kind             = "lxc"
      backend          = "ifupdown"
      mac              = "bc:24:11:a3:52:17"
      ifname           = "vfct117"
      secondary_mac    = "be:24:11:a3:52:17"
      secondary_ifname = "vfct117b"
      primary_member   = "vf1"
      addresses        = ["2001:db8::117/64"]
    }
    phase                = "staged"
    ipv6_proxy_neighbors = ["2001:db8::217"]
  }

  assert {
    condition = (
      pveguest_network_interface.eth0.backend == "ifupdown" &&
      pveguest_network_interface.eth0.name == "eth0" &&
      pveguest_network_interface.eth0.mac == "bc:24:11:a3:52:17" &&
      pveguest_network_interface.eth0.phase == "staged" &&
      pveguest_network_interface.eth0.dhcp4 &&
      pveguest_network_interface.eth0.slaac &&
      toset(pveguest_network_interface.eth0.addresses) == toset(["2001:db8::117/64"]) &&
      toset(pveguest_network_interface.eth0.dns_servers) == toset(["2001:db8::53"]) &&
      toset(pveguest_network_interface.eth0.ipv6_proxy_neighbors) == toset(["2001:db8::217"])
    )
    error_message = "The LXC bond must stage the existing eth0 identity, addresses, DNS, and proxy neighbors."
  }

  assert {
    condition = (
      { for member in pveguest_network_interface.eth0.bond.members : member.name => member.mac } == {
        vf0 = "bc:24:11:a3:52:17"
        vf1 = "be:24:11:a3:52:17"
      } &&
      pveguest_network_interface.eth0.bond.primary == "vf1" &&
      pveguest_network_interface.eth0.bond.miimon == 100 &&
      pveguest_network_interface.eth0.bond.fail_over_mac == "active" &&
      pveguest_network_interface.eth0.bond.primary_reselect == "failure"
    )
    error_message = "The LXC bond must use both VF MAC addresses and select vf1 as primary with the declared failover settings."
  }

  assert {
    condition = pveguest_container_options.hostnic[0].options == tomap({
      hostnic0 = "link=vfct117,name=vf0"
      hostnic1 = "link=vfct117b,name=vf1"
    })
    error_message = "The container must attach both VFs under their bond member names."
  }
}

run "vm_bond_uses_netplan" {
  command = plan

  variables {
    guest = {
      vmid           = 124
      kind           = "qemu"
      backend        = "netplan"
      mac            = "bc:24:11:a3:52:7c"
      secondary_mac  = "be:24:11:a3:52:7c"
      primary_member = "vf0"
      addresses      = ["2001:db8::124/64"]
    }
    phase = "active"
  }

  assert {
    condition = (
      pveguest_network_interface.eth0.kind == "qemu" &&
      pveguest_network_interface.eth0.backend == "netplan" &&
      pveguest_network_interface.eth0.phase == "active" &&
      pveguest_network_interface.eth0.bond.primary == "vf0" &&
      { for member in pveguest_network_interface.eth0.bond.members : member.name => member.mac } == {
        vf0 = "bc:24:11:a3:52:7c"
        vf1 = "be:24:11:a3:52:7c"
      } &&
      length(pveguest_container_options.hostnic) == 0
    )
    error_message = "The VM must configure both bond members in netplan without LXC attachments."
  }
}
