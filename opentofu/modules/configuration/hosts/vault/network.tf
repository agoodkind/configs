locals {
  guest_networks = {
    debianct = {
      vmid    = 100
      index   = 1
      kind    = "lxc"
      backend = "ifupdown"
      driver  = "iavf"
      ifname  = "vfct100"
      mac     = "bc:24:11:73:b0:3c"
    }
    unifi = {
      vmid    = 102
      index   = 2
      kind    = "lxc"
      backend = "ifupdown"
      driver  = "iavf"
      ifname  = "vfct102"
      mac     = "bc:24:11:9e:dc:6d"
    }
    proxy = {
      vmid    = 110
      index   = 3
      kind    = "lxc"
      backend = "networkd"
      driver  = "iavf"
      ifname  = "vfct110"
      mac     = "bc:24:11:1d:2c:0f"
    }
    tack = {
      vmid    = 117
      index   = 4
      kind    = "lxc"
      backend = "ifupdown"
      driver  = "iavf"
      ifname  = "vfct117"
      mac     = "bc:24:11:a3:52:17"
    }
    seaweedfs = {
      vmid    = 118
      index   = 5
      kind    = "lxc"
      backend = "ifupdown"
      driver  = "iavf"
      ifname  = "vfct118"
      mac     = "bc:24:11:00:01:18"
    }
    tack_data1 = {
      vmid    = 120
      index   = 6
      kind    = "lxc"
      backend = "ifupdown"
      driver  = "iavf"
      ifname  = "vfct120"
      mac     = "bc:24:11:a3:52:20"
    }
    tack_data2 = {
      vmid    = 121
      index   = 7
      kind    = "lxc"
      backend = "ifupdown"
      driver  = "iavf"
      ifname  = "vfct121"
      mac     = "bc:24:11:a3:52:21"
    }
    tack_data3 = {
      vmid    = 122
      index   = 8
      kind    = "lxc"
      backend = "ifupdown"
      driver  = "iavf"
      ifname  = "vfct122"
      mac     = "bc:24:11:a3:52:22"
    }
    tack_app2 = {
      vmid    = 123
      index   = 9
      kind    = "lxc"
      backend = "ifupdown"
      driver  = "iavf"
      ifname  = "vfct123"
      mac     = "bc:24:11:a3:52:23"
    }
    tack_search1 = {
      vmid    = 125
      index   = 10
      kind    = "lxc"
      backend = "ifupdown"
      driver  = "iavf"
      ifname  = "vfct125"
      mac     = "bc:24:11:a3:52:24"
    }
    tack_k3s = {
      vmid    = 124
      index   = 0
      kind    = "qemu"
      backend = "netplan"
      driver  = "vfio-pci"
      ifname  = null
      mac     = "bc:24:11:a3:52:7c"
    }
  }
}

output "guest_networks" { value = local.guest_networks }
output "guest_pf" { value = "enp2s0f0np0" }
