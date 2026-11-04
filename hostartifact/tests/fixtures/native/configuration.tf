module "services" { source = "__SERVICES__" }
module "defaults" { source = "__DEFAULTS__" }
module "host" { source = "__HOST__" }
output "service_mapping" { value = module.services.service_mapping }
output "artifact_vars" { value = merge(module.defaults.vars, module.host.vars) }
