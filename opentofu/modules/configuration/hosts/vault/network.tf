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
    # Release this VF and these addresses after the replacement Kubernetes VM serves unifi clients and private administration.
    unifi = {
      vmid      = 102
      index     = 2
      kind      = "lxc"
      backend   = "ifupdown"
      driver    = "iavf"
      ifname    = "vfct102"
      mac       = "bc:24:11:9e:dc:6d"
      addresses = ["10.250.0.102/24", "3d06:bad:b01::102/64"]
    }
    # Release this VF and these addresses after the replacement Kubernetes VM serves proxy clients and private administration.
    proxy = {
      vmid      = 110
      index     = 3
      kind      = "lxc"
      backend   = "networkd"
      driver    = "iavf"
      ifname    = "vfct110"
      mac       = "bc:24:11:1d:2c:0f"
      addresses = ["10.250.0.110/24", "3d06:bad:b01::110/64"]
    }
    # Release this VF and these addresses after the replacement Kubernetes VM serves tack clients and private administration.
    tack = {
      vmid      = 117
      index     = 4
      kind      = "lxc"
      backend   = "ifupdown"
      driver    = "iavf"
      ifname    = "vfct117"
      mac       = "bc:24:11:a3:52:17"
      addresses = ["10.250.0.117/24", "3d06:bad:b01::117/64"]
    }
    # Release this VF and these addresses after the replacement Kubernetes VM serves seaweedfs clients and private administration.
    seaweedfs = {
      vmid      = 118
      index     = 5
      kind      = "lxc"
      backend   = "ifupdown"
      driver    = "iavf"
      ifname    = "vfct118"
      mac       = "bc:24:11:00:01:18"
      addresses = ["3d06:bad:b01::118/64"]
    }
    # Release this VF and these addresses after the replacement Kubernetes VM serves tack data1 clients and private administration.
    tack_data1 = {
      vmid      = 120
      index     = 6
      kind      = "lxc"
      backend   = "ifupdown"
      driver    = "iavf"
      ifname    = "vfct120"
      mac       = "bc:24:11:a3:52:20"
      addresses = ["3d06:bad:b01::120/64"]
    }
    # Release this VF and these addresses after the replacement Kubernetes VM serves tack data2 clients and private administration.
    tack_data2 = {
      vmid      = 121
      index     = 7
      kind      = "lxc"
      backend   = "ifupdown"
      driver    = "iavf"
      ifname    = "vfct121"
      mac       = "bc:24:11:a3:52:21"
      addresses = ["3d06:bad:b01::121/64"]
    }
    # Release this VF and these addresses after the replacement Kubernetes VM serves tack data3 clients and private administration.
    tack_data3 = {
      vmid      = 122
      index     = 8
      kind      = "lxc"
      backend   = "ifupdown"
      driver    = "iavf"
      ifname    = "vfct122"
      mac       = "bc:24:11:a3:52:22"
      addresses = ["3d06:bad:b01::122/64"]
    }
    # Release this VF and these addresses after the replacement Kubernetes VM serves tack app2 clients and private administration.
    tack_app2 = {
      vmid      = 123
      index     = 9
      kind      = "lxc"
      backend   = "ifupdown"
      driver    = "iavf"
      ifname    = "vfct123"
      mac       = "bc:24:11:a3:52:23"
      addresses = ["10.250.0.123/24", "3d06:bad:b01::123/64"]
    }
    # Release this VF and these addresses after the replacement Kubernetes VM serves tack search1 clients and private administration.
    tack_search1 = {
      vmid      = 125
      index     = 10
      kind      = "lxc"
      backend   = "ifupdown"
      driver    = "iavf"
      ifname    = "vfct125"
      mac       = "bc:24:11:a3:52:24"
      addresses = ["3d06:bad:b01::125/64"]
    }
    # Remove the additional tack_k3s addresses after Tack, DNS, and private administration use the replacements.
    tack_k3s = {
      vmid      = 124
      index     = 0
      kind      = "qemu"
      backend   = "netplan"
      driver    = "vfio-pci"
      ifname    = null
      mac       = "bc:24:11:a3:52:7c"
      addresses = ["10.250.0.124/24", "3d06:bad:b01::124/64"]
    }
  }
}

output "guest_networks" { value = local.guest_networks }
output "guest_pf" { value = "enp2s0f0np0" }
