output "app_configuration" {
  value      = local.public
  depends_on = [pveguest_systemd_unit.bootstrap]
}
output "health_unit_ids" {
  value = { data = pveguest_systemd_unit.runtime["data"].name, search = var.search_ready, bootstrap = pveguest_systemd_unit.bootstrap.name }
}
