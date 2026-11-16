locals {
  files = {
    "data/compose.yaml"            = yamlencode(local.data_compose)
    "data/bootstrap.sh"            = file("${path.module}/files/bootstrap.sh")
    "data/yugabyte-entrypoint.sh"  = file("${path.module}/files/yugabyte-entrypoint.sh")
    "data/yugabyte-healthcheck.sh" = file("${path.module}/files/yugabyte-healthcheck.sh")
    "data/tserver.flags"           = file("${path.module}/files/tserver.flags")
  }
}
resource "pveguest_file" "secret" {
  for_each           = local.secret_files
  node               = local.guests[each.value.guest].node
  vmid               = local.guests[each.value.guest].vmid
  kind               = local.guests[each.value.guest].kind
  path               = each.value.path
  mode               = "0600"
  content_wo         = var.secrets[each.value.key]
  content_wo_version = var.secret_versions[each.value.key]
}

resource "pveguest_file" "configuration" {
  for_each = local.files
  node     = local.guests[split("/", each.key)[0]].node
  vmid     = local.guests[split("/", each.key)[0]].vmid
  kind     = local.guests[split("/", each.key)[0]].kind
  path     = "${local.directory}/${split("/", each.key)[0]}/${split("/", each.key)[1]}"
  mode     = endswith(each.key, ".sh") ? "0755" : "0644"
  content  = each.value
}

resource "pveguest_download" "overlay" {
  for_each = local.overlays
  node     = var.data_guest.node
  vmid     = var.data_guest.vmid
  kind     = var.data_guest.kind
  fetch    = "controller"
  url      = "https://raw.githubusercontent.com/agoodkind/tack/${var.release_commit}/${each.key}"
  sha256   = each.value
  path     = "${local.directory}/${each.key}"
  mode     = "0755"
}
