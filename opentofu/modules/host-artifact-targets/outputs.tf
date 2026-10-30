output "workspaces" { value = sort(keys(local.catalog)) }

output "catalog" { value = local.catalog }

output "target" { value = one(values(local.selected)) }
