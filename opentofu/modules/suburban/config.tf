variable "routing_networks" {
  description = "Routing simulator networks from testbed_routing_networks, keyed by network name."
  type        = map(object({ bridge = string }))
}

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

output "host_networks" {
  description = "Suburban host bridges merged with the routing simulator bridges, keyed by network name."
  value = merge(
    local.bridges,
    { for name, network in var.routing_networks : name => network.bridge },
  )
}
