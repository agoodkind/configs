module "artifacts" { source = "../../artifacts" }
module "application" { source = "../application" }

output "application_identity" { value = module.application.identity }
output "vm_recipe" { value = module.application.vm_recipe }

locals { release_key = "74576c94ab9ec6662e9634978d72cb6f06de4470" }
output "release" { value = module.artifacts.tack_releases[local.release_key] }
output "backup_check_release" { value = module.artifacts.tack_backup_check_release }
output "cloudflared_image" { value = module.artifacts.cloudflared_image }

output "vm_capacity" { value = { cores = 4, memory_mib = 8192, disk_gib = 40 } }
output "application_replicas" { value = 2 }
output "application_strategy" { value = "RollingUpdate" }
output "connector_count" { value = 2 }
output "snapshots_enabled" { value = true }
