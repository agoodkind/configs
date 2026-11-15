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

locals {
  files = {
    "data/compose.yaml"   = yamlencode(local.data_compose)
    "search/compose.yaml" = templatefile("${path.module}/files/search-compose.yaml.tftpl", { guest = var.search_guest })
    "search/opensearch.yml" = templatefile("${path.module}/files/opensearch.yml.tftpl", {
      admin_dn = var.search_admin_dn, node_dn = var.search_node_dn
    })
    "data/bootstrap.sh"            = file("${path.module}/files/bootstrap.sh")
    "data/yugabyte-entrypoint.sh"  = file("${path.module}/files/yugabyte-entrypoint.sh")
    "data/yugabyte-healthcheck.sh" = file("${path.module}/files/yugabyte-healthcheck.sh")
    "data/tserver.flags"           = file("${path.module}/files/tserver.flags")
    "search/security.sh"           = file("${path.module}/files/security.sh")
    "search/check-host.sh"         = file("${path.module}/files/check-host.sh")
    "search/roles.yml"             = file("${path.module}/files/roles.yml")
    "search/roles_mapping.yml"     = yamlencode({ _meta = { type = "rolesmapping", config_version = 2 }, all_access = { reserved = false, users = [var.search_username] } })
    "search/config.yml"            = file("${path.module}/files/security-config.yml")
    "search/action_groups.yml"     = yamlencode({ _meta = { type = "actiongroups", config_version = 2 } })
    "search/tenants.yml"           = yamlencode({ _meta = { type = "tenants", config_version = 2 } })
    "search/nodes_dn.yml"          = yamlencode({ _meta = { type = "nodesdn", config_version = 2 } })
    "search/allowlist.yml"         = yamlencode({ _meta = { type = "allowlist", config_version = 2 }, config = { enabled = false, requests = {} } })
  }
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

resource "pveguest_file" "search_users" {
  node               = var.search_guest.node
  vmid               = var.search_guest.vmid
  kind               = var.search_guest.kind
  path               = "${local.directory}/search/internal_users.yml"
  mode               = "0600"
  content_wo         = yamlencode({ _meta = { type = "internalusers", config_version = 2 }, (var.search_username) = { hash = var.secrets["search_password_hash"], reserved = false, backend_roles = [] } })
  content_wo_version = var.secret_versions["search_password_hash"]
}
