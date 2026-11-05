locals {
  bridges = {
    isp_astound_suburban   = "vmbr7"
    isp_att_suburban       = "vmbr5"
    isp_mbrains_suburban   = "vmbr6"
    isp_routed_suburban    = "vmbr8"
    isp_webpass_suburban   = "vmbr4"
    mwan_suburban          = "vmbr2"
    trunk_suburban         = "vmbrtrunk"
    vm_management_suburban = "vmbr1"
  }
}

output "bridges" {
  description = "Bridge names of the eight Suburban host bridges, keyed by bridge resource label."
  value       = local.bridges
}
