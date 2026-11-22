module "seaweedfs" { source = "../../../configuration/services/seaweedfs" }
module "tack" { source = "../../../configuration/services/tack" }
module "tack_app2" { source = "../../../configuration/services/tack_app2" }
module "tack_data1" { source = "../../../configuration/services/tack_data1" }
module "tack_data2" { source = "../../../configuration/services/tack_data2" }
module "tack_data3" { source = "../../../configuration/services/tack_data3" }
module "tack_search1" { source = "../../../configuration/services/tack_search1" }
module "poweredge_hypervisor" { source = "../../../poweredge/services/poweredge_hypervisor" }
locals {
  service_mapping = {
    poweredge_hypervisor = module.poweredge_hypervisor.value
    seaweedfs            = module.seaweedfs.value
    tack                 = module.tack.value
    tack_app2            = module.tack_app2.value
    tack_data1           = module.tack_data1.value
    tack_data2           = module.tack_data2.value
    tack_data3           = module.tack_data3.value
    tack_search1         = module.tack_search1.value
  }
}
output "service_mapping" { value = local.service_mapping }
locals {
  lock_targets = {
    "lock_free_types" = ["cloudflare_dns_record", "cloudflare_certificate_pack"]
  }
}
output "lock_targets" { value = local.lock_targets }

output "managed_guests" { value = {} }
