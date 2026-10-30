output "workspaces" { value = local.workspaces }

output "target" { value = one(values(local.targets)) }
