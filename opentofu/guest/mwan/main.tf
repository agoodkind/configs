terraform {
  required_providers {
    pveguest = {
      source  = "tofu.home.arpa/agoodkind/pveguest"
      version = "0.1.0"
    }
    mwan = {
      source = "tofu.home.arpa/agoodkind/mwan"
    }
  }
}

variable "node" {
  type = string
}

variable "vmid" {
  type = number
}

variable "release_version" {
  type = string
}

variable "architecture" {
  type = string
}

variable "role" {
  type = string
}

data "mwan_release" "this" {
  version = var.release_version
}

data "mwan_role" "this" {
  role = var.role
}

locals {
  kind      = "lxc"
  stack_dir = "/var/cache/mwan/stack"

  architecture = data.mwan_release.this.architectures[var.architecture]

  files = { for file in data.mwan_role.this.files : file.path => file }
  units = { for unit in data.mwan_role.this.units : unit.name => unit }

  yang_modules = {
    for module in data.mwan_role.this.yang_modules : split("@", module.file)[0] => module
  }

  sysrepo_data = {
    for entry in data.mwan_role.this.sysrepo_data : "${entry.datastore}-${entry.module}" => entry
  }

  yang_order = [
    "ietf-yang-types",
    "ietf-inet-types",
    "iana-if-type",
    "ietf-interfaces",
    "ietf-ip",
    "ietf-routing",
    "ietf-nat",
    "goodkind-mwan-steering",
  ]

  yang_steps = {
    for name in local.yang_order : name => {
      for key, module in local.yang_modules : key => module if key == name
    }
  }

  yang_extra = {
    for key, module in local.yang_modules : key => module if !contains(local.yang_order, key)
  }

  stack_debs = length(local.yang_modules) > 0 ? local.architecture.stack_debs : {}

  mask_units = var.role == "wan" ? toset(["nftables.service"]) : toset([])

  owned_units = {
    for name, unit in local.units : name => anytrue([
      for path in unit.files : endswith(path, ".service")
    ])
  }

  runtime_write_ids = merge(
    { binary = pveguest_download.mwan.write_id },
    { for name, deb in pveguest_download.stack_deb : "deb-${name}" => deb.write_id },
  )
}

resource "pveguest_download" "mwan" {
  node = var.node
  vmid = var.vmid
  kind = local.kind

  fetch          = "controller"
  url            = local.architecture.mwan_url
  sha256         = local.architecture.mwan_sha256
  archive_member = data.mwan_release.this.archive_member
  path           = data.mwan_role.this.binary_path
  mode           = "0755"
}

resource "pveguest_download" "stack_deb" {
  for_each = local.stack_debs

  node = var.node
  vmid = var.vmid
  kind = local.kind

  fetch          = "controller"
  url            = local.architecture.stack_url
  sha256         = local.architecture.stack_sha256
  archive_member = each.value
  path           = "${local.stack_dir}/${basename(each.value)}"
}

resource "pveguest_deb_packages" "stack" {
  for_each = length(local.stack_debs) > 0 ? toset(["stack"]) : toset([])

  node = var.node
  vmid = var.vmid
  kind = local.kind

  packages = { for name, deb in pveguest_download.stack_deb : name => deb.path }
}

resource "pveguest_file" "role" {
  for_each = local.files

  node = var.node
  vmid = var.vmid
  kind = local.kind

  path    = each.value.path
  content = each.value.content
  mode    = each.value.mode
}

resource "pveguest_file" "yang" {
  for_each = local.yang_modules

  node = var.node
  vmid = var.vmid
  kind = local.kind

  path    = each.value.path
  content = each.value.content
  mode    = each.value.mode
}

resource "pveguest_link" "mask" {
  for_each = local.mask_units

  node = var.node
  vmid = var.vmid
  kind = local.kind

  path   = "/etc/systemd/system/${each.key}"
  target = "/dev/null"
}

resource "pveguest_sysrepo_module" "ietf_yang_types" {
  for_each = local.yang_steps["ietf-yang-types"]

  node = var.node
  vmid = var.vmid
  kind = local.kind

  path     = each.value.path
  features = toset(each.value.features)
  update   = each.value.update

  depends_on = [
    pveguest_file.yang,
    pveguest_deb_packages.stack,
  ]
}

resource "pveguest_sysrepo_module" "ietf_inet_types" {
  for_each = local.yang_steps["ietf-inet-types"]

  node = var.node
  vmid = var.vmid
  kind = local.kind

  path     = each.value.path
  features = toset(each.value.features)
  update   = each.value.update

  depends_on = [
    pveguest_file.yang,
    pveguest_deb_packages.stack,
    pveguest_sysrepo_module.ietf_yang_types,
  ]
}

resource "pveguest_sysrepo_module" "iana_if_type" {
  for_each = local.yang_steps["iana-if-type"]

  node = var.node
  vmid = var.vmid
  kind = local.kind

  path     = each.value.path
  features = toset(each.value.features)
  update   = each.value.update

  depends_on = [
    pveguest_file.yang,
    pveguest_deb_packages.stack,
    pveguest_sysrepo_module.ietf_inet_types,
  ]
}

resource "pveguest_sysrepo_module" "ietf_interfaces" {
  for_each = local.yang_steps["ietf-interfaces"]

  node = var.node
  vmid = var.vmid
  kind = local.kind

  path     = each.value.path
  features = toset(each.value.features)
  update   = each.value.update

  depends_on = [
    pveguest_file.yang,
    pveguest_deb_packages.stack,
    pveguest_sysrepo_module.iana_if_type,
  ]
}

resource "pveguest_sysrepo_module" "ietf_ip" {
  for_each = local.yang_steps["ietf-ip"]

  node = var.node
  vmid = var.vmid
  kind = local.kind

  path     = each.value.path
  features = toset(each.value.features)
  update   = each.value.update

  depends_on = [
    pveguest_file.yang,
    pveguest_deb_packages.stack,
    pveguest_sysrepo_module.ietf_interfaces,
  ]
}

resource "pveguest_sysrepo_module" "ietf_routing" {
  for_each = local.yang_steps["ietf-routing"]

  node = var.node
  vmid = var.vmid
  kind = local.kind

  path     = each.value.path
  features = toset(each.value.features)
  update   = each.value.update

  depends_on = [
    pveguest_file.yang,
    pveguest_deb_packages.stack,
    pveguest_sysrepo_module.ietf_ip,
  ]
}

resource "pveguest_sysrepo_module" "ietf_nat" {
  for_each = local.yang_steps["ietf-nat"]

  node = var.node
  vmid = var.vmid
  kind = local.kind

  path     = each.value.path
  features = toset(each.value.features)
  update   = each.value.update

  depends_on = [
    pveguest_file.yang,
    pveguest_deb_packages.stack,
    pveguest_sysrepo_module.ietf_routing,
  ]
}

resource "pveguest_sysrepo_module" "goodkind_mwan_steering" {
  for_each = local.yang_steps["goodkind-mwan-steering"]

  node = var.node
  vmid = var.vmid
  kind = local.kind

  path     = each.value.path
  features = toset(each.value.features)
  update   = each.value.update

  depends_on = [
    pveguest_file.yang,
    pveguest_deb_packages.stack,
    pveguest_sysrepo_module.ietf_nat,
  ]
}

resource "pveguest_sysrepo_module" "extra" {
  for_each = local.yang_extra

  node = var.node
  vmid = var.vmid
  kind = local.kind

  path     = each.value.path
  features = toset(each.value.features)
  update   = each.value.update

  depends_on = [
    pveguest_file.yang,
    pveguest_deb_packages.stack,
    pveguest_sysrepo_module.goodkind_mwan_steering,
  ]
}

resource "pveguest_sysrepo_data" "role" {
  for_each = local.sysrepo_data

  node = var.node
  vmid = var.vmid
  kind = local.kind

  datastore = each.value.datastore
  module    = each.value.module
  content   = each.value.content

  depends_on = [
    pveguest_deb_packages.stack,
    pveguest_sysrepo_module.ietf_yang_types,
    pveguest_sysrepo_module.ietf_inet_types,
    pveguest_sysrepo_module.iana_if_type,
    pveguest_sysrepo_module.ietf_interfaces,
    pveguest_sysrepo_module.ietf_ip,
    pveguest_sysrepo_module.ietf_routing,
    pveguest_sysrepo_module.ietf_nat,
    pveguest_sysrepo_module.goodkind_mwan_steering,
    pveguest_sysrepo_module.extra,
  ]
}

resource "pveguest_systemd_unit" "role" {
  for_each = local.units

  node = var.node
  vmid = var.vmid
  kind = local.kind

  name    = each.key
  enabled = each.value.enabled
  active  = each.value.active

  restart_on = merge(
    { for path in each.value.files : path => pveguest_file.role[path].write_id },
    { for key, write_id in local.runtime_write_ids : key => write_id if local.owned_units[each.key] },
  )

  depends_on = [
    pveguest_sysrepo_data.role,
    pveguest_link.mask,
    pveguest_download.mwan,
    pveguest_deb_packages.stack,
  ]
}
