output "endpoint" { value = var.endpoint }
output "ready" { value = pveguest_systemd_unit.runtime["search"].name }
