module "docker_guest" {
  source   = "../../modules/docker-guest"
  for_each = module.configuration.docker_guests

  node = local.guests[each.key].node
  vmid = local.guests[each.key].vmid
  kind = local.guests[each.key].kind

  engine_versions  = each.value.engine_versions
  repository       = each.value.repository
  uplink_interface = each.value.uplink_interface
  ndp_proxy_prefix = each.value.ndp_proxy_prefix
  sysctl_path      = each.value.sysctl_path

  depends_on = [module.base]
}
