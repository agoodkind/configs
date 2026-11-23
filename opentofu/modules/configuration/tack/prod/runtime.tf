module "artifacts" { source = "../../artifacts" }
module "application" { source = "../application" }

output "application_identity" { value = module.application.identity }
output "vm_recipe" { value = module.application.vm_recipe }

locals { release_commit = "779e7618fe694a56f4100bacece9c3cb8d646f1f" }
output "release" { value = module.artifacts.tack_releases[local.release_commit] }
output "backup_check_release" { value = module.artifacts.tack_backup_check_release }
output "cloudflared_image" { value = module.artifacts.cloudflared_image }

output "vm_capacity" { value = { cores = 4, memory_mib = 8192, disk_gib = 40 } }
output "application_replicas" { value = 2 }
output "application_strategy" { value = "RollingUpdate" }
output "connector_count" { value = 2 }
output "snapshots_enabled" { value = true }
