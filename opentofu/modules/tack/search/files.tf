locals {
  files = {
    "search/compose.yaml" = templatefile("${path.module}/files/search-compose.yaml.tftpl", { image = var.image })
    "search/opensearch.yml" = templatefile("${path.module}/files/opensearch.yml.tftpl", {
      admin_dn = var.search_admin_dn, node_dn = var.search_node_dn, node_address = var.search_guest.ipv6
    })
    "search/security.sh"       = file("${path.module}/files/security.sh")
    "search/check-host.sh"     = file("${path.module}/files/check-host.sh")
    "search/roles.yml"         = file("${path.module}/files/roles.yml")
    "search/roles_mapping.yml" = yamlencode({ _meta = { type = "rolesmapping", config_version = 2 }, all_access = { reserved = false, users = [var.search_username] } })
    "search/config.yml"        = file("${path.module}/files/security-config.yml")
    "search/action_groups.yml" = yamlencode({ _meta = { type = "actiongroups", config_version = 2 } })
    "search/tenants.yml"       = yamlencode({ _meta = { type = "tenants", config_version = 2 } })
    "search/nodes_dn.yml"      = yamlencode({ _meta = { type = "nodesdn", config_version = 2 } })
    "search/allowlist.yml"     = yamlencode({ _meta = { type = "allowlist", config_version = 2 }, config = { enabled = false, requests = {} } })
  }
}
resource "pveguest_file" "secret" {
  for_each           = local.secret_files
  node               = local.guests[each.value.guest].node
  vmid               = local.guests[each.value.guest].vmid
  kind               = local.guests[each.value.guest].kind
  path               = each.value.path
  mode               = "0600"
  owner              = module.runtime_identity.name
  group              = module.runtime_identity.name
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

resource "pveguest_file" "search_users" {
  node               = var.search_guest.node
  vmid               = var.search_guest.vmid
  kind               = var.search_guest.kind
  path               = "${local.directory}/search/internal_users.yml"
  mode               = "0600"
  owner              = module.runtime_identity.name
  group              = module.runtime_identity.name
  content_wo         = yamlencode({ _meta = { type = "internalusers", config_version = 2 }, (var.search_username) = { hash = var.secrets["search_password_hash"], reserved = false, backend_roles = [] } })
  content_wo_version = var.secret_versions["search_password_hash"]
}

resource "pveguest_file" "http_client_trust" {
  node               = var.search_guest.node
  vmid               = var.search_guest.vmid
  kind               = var.search_guest.kind
  path               = "${local.directory}/search/http-client-trust.pem"
  mode               = "0600"
  owner              = module.runtime_identity.name
  group              = module.runtime_identity.name
  content_wo         = "${trimspace(var.secrets["search_ca"])}\n${trimspace(var.secrets["search_admin_certificate"])}\n"
  content_wo_version = var.secret_versions["search_ca"] + var.secret_versions["search_admin_certificate"]
}
