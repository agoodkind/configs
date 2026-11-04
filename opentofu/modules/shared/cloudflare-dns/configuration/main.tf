module "debianct" { source = "../../../vault/services/debianct" }
module "proxy" { source = "../../../vault/services/proxy" }
module "seaweedfs" { source = "../../../vault/services/seaweedfs" }
module "tack" { source = "../../../vault/services/tack" }
module "tack_app2" { source = "../../../vault/services/tack_app2" }
module "tack_data1" { source = "../../../vault/services/tack_data1" }
module "tack_data2" { source = "../../../vault/services/tack_data2" }
module "tack_data3" { source = "../../../vault/services/tack_data3" }
module "tack_search1" { source = "../../../vault/services/tack_search1" }
locals {
  service_mapping = {
    debianct     = module.debianct.value
    proxy        = module.proxy.value
    seaweedfs    = module.seaweedfs.value
    tack         = module.tack.value
    tack_app2    = module.tack_app2.value
    tack_data1   = module.tack_data1.value
    tack_data2   = module.tack_data2.value
    tack_data3   = module.tack_data3.value
    tack_search1 = module.tack_search1.value
  }
}
output "service_mapping" { value = local.service_mapping }
locals {
  lock_targets = {
    "lock_free_types" = ["cloudflare_dns_record"]
  }
}
output "lock_targets" { value = local.lock_targets }
output "proxy_public_key" { value = module.proxy.upstream_public_key }
output "proxy_address" { value = module.proxy.value.ipv6 }
