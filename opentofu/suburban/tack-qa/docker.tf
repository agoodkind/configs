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

module "docker_guest_paths" {
  source   = "../../modules/docker-guest/paths"
  for_each = module.configuration.docker_guests

  sysctl_path = each.value.sysctl_path
}

locals {
  docker_guest_import_prefixes = {
    for key in keys(module.configuration.docker_guests) :
    key => "${local.guests[key].node}/${local.guests[key].vmid}/${local.guests[key].kind}"
  }

  docker_guest_file_imports = merge([
    for key, prefix in local.docker_guest_import_prefixes : {
      for file_key, file_path in module.docker_guest_paths[key].file_paths :
      "${key}/${file_key}" => { guest = key, file = file_key, id = "${prefix}/${file_path}" }
    }
  ]...)
}

import {
  for_each = local.docker_guest_file_imports
  to       = module.docker_guest[each.value.guest].pveguest_file.docker[each.value.file]
  id       = each.value.id
}

import {
  for_each = local.docker_guest_import_prefixes
  to       = module.docker_guest[each.key].pveguest_systemd_unit.sysctl
  id       = "${each.value}/systemd-sysctl.service"
}

import {
  for_each = local.docker_guest_import_prefixes
  to       = module.docker_guest[each.key].pveguest_systemd_unit.ndppd
  id       = "${each.value}/ndppd.service"
}

import {
  for_each = local.docker_guest_import_prefixes
  to       = module.docker_guest[each.key].pveguest_systemd_unit.docker
  id       = "${each.value}/docker.service"
}

import {
  for_each = local.docker_guest_import_prefixes
  to       = module.docker_guest[each.key].pveguest_systemd_unit.prune_timer
  id       = "${each.value}/docker-prune.timer"
}
