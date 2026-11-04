locals {
  kernel_modules = [
    "nf_tables", "nfnetlink", "nf_conntrack", "nf_nat", "nft_chain_nat",
    "nft_masq", "nft_ct", "nft_nat", "nf_defrag_ipv4", "nf_defrag_ipv6",
    "nft_log", "nf_log_syslog", "nft_limit", "nft_numgen", "nft_hash",
    "sch_ingress", "cls_bpf", "8021q", "sit",
  ]
}

output "kernel_modules" {
  description = "Kernel modules that PowerEdge installs and that the Proxmox overlay allows containers to load."
  value       = local.kernel_modules
}
