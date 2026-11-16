locals {
  application_secret_files = {
    database_url       = "/var/lib/tack/secrets/DATABASE_URL"
    audit_operator_dsn = "/var/lib/tack/secrets/AUDIT_OPERATOR_DSN"
    search_password    = "/var/lib/tack/secrets/OPENSEARCH_PASSWORD"
    search_cursor_key  = "/var/lib/tack/secrets/OPENSEARCH_CURSOR_KEY"
    search_ca          = "/etc/tack/runtime/search-ca.crt"
    fdb_bootstrap      = "/var/lib/tack/bootstrap/fdb.cluster"
  }
}

resource "pveguest_file" "application_secret" {
  for_each = local.application_secret_files

  node               = var.guest.node
  vmid               = var.guest.vmid
  kind               = "qemu"
  path               = each.value
  mode               = "0600"
  content_wo         = "${chomp(var.application_secrets[each.key])}\n"
  content_wo_version = var.application_secret_versions[each.key]
}
