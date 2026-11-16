module "artifacts" { source = "../../artifacts" }
module "application" { source = "../application" }

output "application_identity" { value = module.application.identity }
output "store_images" { value = module.artifacts.tack_store_images }
output "overlays" { value = module.artifacts.tack_overlays }

output "release" {
  value = {
    commit          = "74576c94ab9ec6662e9634978d72cb6f06de4470"
    server_digest   = "sha256:c588661aca86bc3cb943d231b5eb04a0fed76c02fbd4b9eb44e6ca191db2957e"
    consumer_digest = "sha256:59911e352a6a7012c6b4b95461a4855fb46e11de250a2a210cb623f7e2d0e3e2"
    accountable     = "alex@goodkind.io"
  }
}

output "vm_capacity" { value = { cores = 2, memory_mib = 4096, disk_gib = 40 } }
output "application_replicas" { value = 1 }
output "application_strategy" { value = "Recreate" }
output "connector_count" { value = 1 }
output "snapshots_enabled" { value = false }
