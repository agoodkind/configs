locals {
  guest_networks = {
    debianct = {
      vmid             = 100
      index            = 1
      kind             = "lxc"
      backend          = "ifupdown"
      driver           = "iavf"
      ifname           = "vfct100"
      mac              = "bc:24:11:73:b0:3c"
      addresses        = []
      secondary_mac    = "be:24:11:73:b0:3c"
      secondary_ifname = "vfct100b"
      primary_member   = "vf0"
    }
    # Remove these VF assignments after a Kubernetes VM replaces the UniFi LXC.
    unifi = {
      vmid             = 102
      index            = 2
      kind             = "lxc"
      backend          = "ifupdown"
      driver           = "iavf"
      ifname           = "vfct102"
      mac              = "bc:24:11:9e:dc:6d"
      addresses        = []
      secondary_mac    = "be:24:11:9e:dc:6d"
      secondary_ifname = "vfct102b"
      primary_member   = "vf1"
    }
    # Remove these VF assignments and the static IPv6 address after all Kafka clients use the replacement broker.
    tack = {
      vmid             = 117
      index            = 4
      kind             = "lxc"
      backend          = "ifupdown"
      driver           = "iavf"
      ifname           = "vfct117"
      mac              = "bc:24:11:a3:52:17"
      addresses        = ["3d06:bad:b01::117/64"]
      secondary_mac    = "be:24:11:a3:52:17"
      secondary_ifname = "vfct117b"
      primary_member   = "vf1"
    }
    # Remove these VF assignments and addresses after the replacement Kubernetes VM serves seaweedfs clients and private administration.
    seaweedfs = {
      vmid             = 118
      index            = 5
      kind             = "lxc"
      backend          = "ifupdown"
      driver           = "iavf"
      ifname           = "vfct118"
      mac              = "bc:24:11:00:01:18"
      addresses        = ["3d06:bad:b01::118/64"]
      secondary_mac    = "be:24:11:00:01:18"
      secondary_ifname = "vfct118b"
      primary_member   = "vf0"
    }
    # Remove these VF assignments and addresses after the replacement Kubernetes VM serves tack data1 clients and private administration.
    tack_data1 = {
      vmid             = 120
      index            = 6
      kind             = "lxc"
      backend          = "ifupdown"
      driver           = "iavf"
      ifname           = "vfct120"
      mac              = "bc:24:11:a3:52:20"
      addresses        = ["3d06:bad:b01::120/64"]
      secondary_mac    = "be:24:11:a3:52:20"
      secondary_ifname = "vfct120b"
      primary_member   = "vf1"
    }
    # Remove these VF assignments and addresses after the replacement Kubernetes VM serves tack data2 clients and private administration.
    tack_data2 = {
      vmid             = 121
      index            = 7
      kind             = "lxc"
      backend          = "ifupdown"
      driver           = "iavf"
      ifname           = "vfct121"
      mac              = "bc:24:11:a3:52:21"
      addresses        = ["3d06:bad:b01::121/64"]
      secondary_mac    = "be:24:11:a3:52:21"
      secondary_ifname = "vfct121b"
      primary_member   = "vf0"
    }
    # Remove these VF assignments and addresses after the replacement Kubernetes VM serves tack data3 clients and private administration.
    tack_data3 = {
      vmid             = 122
      index            = 8
      kind             = "lxc"
      backend          = "ifupdown"
      driver           = "iavf"
      ifname           = "vfct122"
      mac              = "bc:24:11:a3:52:22"
      addresses        = ["3d06:bad:b01::122/64"]
      secondary_mac    = "be:24:11:a3:52:22"
      secondary_ifname = "vfct122b"
      primary_member   = "vf1"
    }
    # Remove these VF assignments and addresses after the replacement Kubernetes VM serves tack search1 clients and private administration.
    tack_search1 = {
      vmid             = 125
      index            = 10
      kind             = "lxc"
      backend          = "ifupdown"
      driver           = "iavf"
      ifname           = "vfct125"
      mac              = "bc:24:11:a3:52:24"
      addresses        = ["3d06:bad:b01::125/64"]
      secondary_mac    = "be:24:11:a3:52:24"
      secondary_ifname = "vfct125b"
      primary_member   = "vf1"
    }
    # Remove the static tack_k3s IPv6 address after Tack, DNS, and private administration use the replacements.
    tack_k3s = {
      vmid             = 124
      index            = 0
      kind             = "qemu"
      backend          = "netplan"
      driver           = "vfio-pci"
      ifname           = null
      mac              = "bc:24:11:a3:52:7c"
      addresses        = ["3d06:bad:b01::124/64"]
      secondary_mac    = "be:24:11:a3:52:7c"
      secondary_ifname = null
      primary_member   = "vf0"
    }
    tack_k3s_worker_1 = {
      vmid             = 127
      index            = 9
      kind             = "qemu"
      backend          = "netplan"
      driver           = "vfio-pci"
      ifname           = null
      mac              = "bc:24:11:a3:52:7f"
      addresses        = []
      secondary_mac    = "be:24:11:a3:52:7f"
      secondary_ifname = null
      primary_member   = "vf1"
    }
  }
}

output "guest_networks" { value = local.guest_networks }
output "guest_pf" { value = "enp2s0f0np0" }
output "guest_pf_secondary" { value = "enp2s0f1np1" }
