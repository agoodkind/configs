module "artifacts" { source = "../../artifacts" }
module "application" { source = "../application" }

output "application_identity" { value = module.application.identity }
output "vm_recipe" { value = module.application.vm_recipe }
output "store_images" {
  value = merge(module.artifacts.tack_store_images, {
    foundationdb = module.artifacts.foundationdb_images["7.4.7"]
  })
}
output "overlays" { value = module.artifacts.tack_overlays }

locals { release_key = "74576c94ab9ec6662e9634978d72cb6f06de4470" }
output "release" { value = module.artifacts.tack_releases[local.release_key] }
output "cloudflared_image" { value = module.artifacts.cloudflared_image }

output "vm_capacity" { value = { cores = 2, memory_mib = 4096, disk_gib = 40 } }
output "application_replicas" { value = 1 }
output "application_strategy" { value = "Recreate" }
output "connector_count" { value = 1 }
output "snapshots_enabled" { value = false }
output "search_runtime_identity" { value = { name = "tack-search", id = 1000 } }

locals {
  lxc_capacity = {
    tack_qa_suburban      = { memory_mib = 6144, cores = 2, disk_gib = 40 }
    tack_search1_suburban = { memory_mib = 8192, cores = 2, disk_gib = 40 }
  }
}
output "lxc_capacity" { value = local.lxc_capacity }
output "lxc_template_file_id" { value = "${module.suburban_host.guest.image_datastore}:vztmpl/${module.artifacts.debian_lxc_template}" }
