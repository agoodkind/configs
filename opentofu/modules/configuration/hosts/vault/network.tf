locals {
  guest_networks = {
    debianct = {
      vmid      = 100
      index     = 1
      kind      = "lxc"
      backend   = "ifupdown"
      driver    = "iavf"
      ifname    = "vfct100"
      mac       = "bc:24:11:73:b0:3c"
      addresses = []
    }
    # Release this VF after a Kubernetes VM replaces the UniFi LXC.
    unifi = {
      vmid      = 102
      index     = 2
      kind      = "lxc"
      backend   = "ifupdown"
      driver    = "iavf"
      ifname    = "vfct102"
      mac       = "bc:24:11:9e:dc:6d"
      addresses = []
    }
    # Release this VF after the replacement Kubernetes VM serves tack clients and private administration.
    tack = {
      vmid      = 117
      index     = 4
      kind      = "lxc"
      backend   = "ifupdown"
      driver    = "iavf"
      ifname    = "vfct117"
      mac       = "bc:24:11:a3:52:17"
      addresses = []
    }
    # Release this VF after the replacement Kubernetes VM serves seaweedfs clients and private administration.
    seaweedfs = {
      vmid      = 118
      index     = 5
      kind      = "lxc"
      backend   = "ifupdown"
      driver    = "iavf"
      ifname    = "vfct118"
      mac       = "bc:24:11:00:01:18"
      addresses = []
    }
    # Release this VF after the replacement Kubernetes VM serves tack data1 clients and private administration.
    tack_data1 = {
      vmid      = 120
      index     = 6
      kind      = "lxc"
      backend   = "ifupdown"
      driver    = "iavf"
      ifname    = "vfct120"
      mac       = "bc:24:11:a3:52:20"
      addresses = []
    }
    # Release this VF after the replacement Kubernetes VM serves tack data2 clients and private administration.
    tack_data2 = {
      vmid      = 121
      index     = 7
      kind      = "lxc"
      backend   = "ifupdown"
      driver    = "iavf"
      ifname    = "vfct121"
      mac       = "bc:24:11:a3:52:21"
      addresses = []
    }
    # Release this VF after the replacement Kubernetes VM serves tack data3 clients and private administration.
    tack_data3 = {
      vmid      = 122
      index     = 8
      kind      = "lxc"
      backend   = "ifupdown"
      driver    = "iavf"
      ifname    = "vfct122"
      mac       = "bc:24:11:a3:52:22"
      addresses = []
    }
    # Release this VF after the replacement Kubernetes VM serves tack app2 clients and private administration.
    tack_app2 = {
      vmid      = 123
      index     = 9
      kind      = "lxc"
      backend   = "ifupdown"
      driver    = "iavf"
      ifname    = "vfct123"
      mac       = "bc:24:11:a3:52:23"
      addresses = []
    }
    # Release this VF after the replacement Kubernetes VM serves tack search1 clients and private administration.
    tack_search1 = {
      vmid      = 125
      index     = 10
      kind      = "lxc"
      backend   = "ifupdown"
      driver    = "iavf"
      ifname    = "vfct125"
      mac       = "bc:24:11:a3:52:24"
      addresses = []
    }
    tack_k3s = {
      vmid      = 124
      index     = 0
      kind      = "qemu"
      backend   = "netplan"
      driver    = "vfio-pci"
      ifname    = null
      mac       = "bc:24:11:a3:52:7c"
      addresses = []
    }
  }
}

output "guest_networks" { value = local.guest_networks }
output "guest_pf" { value = "enp2s0f0np0" }
