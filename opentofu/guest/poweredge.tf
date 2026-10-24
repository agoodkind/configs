locals {
  poweredge_node             = "poweredge"
  poweredge_template_file_id = "local:vztmpl/debian-13-standard_13.6-1_amd64.tar.zst"
  poweredge_datastore_id     = "vmdata"
  poweredge_mwan_release     = "202610192152-ef-9bc78a1"
  poweredge_nat64_prefix     = "3d06:bad:6464::/96"

  poweredge_kernel_modules = toset([
    "nf_tables", "nfnetlink", "nf_conntrack", "nf_nat", "nft_chain_nat",
    "nft_masq", "nft_ct", "nft_nat", "nf_defrag_ipv4", "nf_defrag_ipv6",
    "nft_log", "nf_log_syslog", "nft_limit", "nft_numgen", "nft_hash",
    "sch_ingress", "cls_bpf", "8021q", "sit",
  ])

  poweredge_guests = {
    gateway = {
      vmid         = 313
      hostname     = "mwan-poweredge"
      cpu_cores    = 16
      memory_mb    = 2048
      disk_size_gb = 8
      devices      = []
      console      = false
      options = {
        hostnic0    = "link=nic1v0,name=sonic"
        hostnic1    = "link=nic2v0,name=etheric"
        hostnic2    = "link=nic1v1,name=transit"
        bpfdelegate = "cmds=map_create;prog_load;btf_load,maps=hash,progs=socket_filter;sched_cls,attachs=cgroup_inet_ingress;tcx_ingress;tcx_egress"
      }
    }
    lan = {
      vmid         = 314
      hostname     = "edge-lan"
      cpu_cores    = 16
      memory_mb    = 1024
      disk_size_gb = 8
      devices      = ["/dev/net/tun", "/dev/vhost-net"]
      console      = true
      options = {
        hostnic0 = "link=nic1v2,name=transit"
        hostnic1 = "link=nic3v0,name=lan"
        hostnic2 = "link=nic0v1,name=lan-out,up=0"
      }
    }
    failover = {
      vmid         = 316
      hostname     = "mwan-failover-poweredge"
      cpu_cores    = 2
      memory_mb    = 512
      disk_size_gb = 4
      devices      = []
      console      = false
      options = {
        hostnic0 = "link=nic1v3,name=eth0"
        hostnic1 = "link=nic1v4,name=eth1"
      }
    }
  }

  poweredge_mwan_roles = {
    gateway = {
      role              = "wan"
      network_json_path = "${path.module}/poweredge/network.json"
      config_toml_path  = "${path.module}/poweredge/config.toml"
      excluded_units    = toset(["nghttpx-wanconfig.service", "rousette.service"])
      sysctl_values     = { "net.ipv4.fwmark_reflect" = "1" }
    }
    failover = {
      role              = "failover"
      network_json_path = null
      config_toml_path  = "${path.module}/poweredge/failover/config.toml"
      excluded_units    = toset([])
      sysctl_values     = {}
    }
  }
}
