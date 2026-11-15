locals {
  directory = "/opt/tack-qa"
  guests    = { data = var.data_guest, search = var.search_guest }
  public = {
    fdb_cluster_contents = "tackqa:tackqa@[${var.data_guest.ipv6}]:4500"
    search_endpoint      = "https://[${var.search_guest.ipv6}]:9200"
    audit_kafka_brokers  = "[${var.data_guest.ipv6}]:9092"
    audit_valid_signers  = var.signing_id
  }
  data_secrets = {
    database_url            = "DATABASE_URL"
    migrator_database_url   = "MIGRATOR_DATABASE_URL"
    superuser_database_url  = "SUPERUSER_DATABASE_URL"
    audit_operator_dsn      = "AUDIT_OPERATOR_DSN"
    audit_reader_dsn        = "AUDIT_READER_DSN"
    audit_redactor_dsn      = "AUDIT_REDACTOR_DSN"
    audit_writer_dsn        = "AUDIT_WRITER_DSN"
    app_password            = "TACK_APP_PASSWORD"
    migrator_password       = "TACK_MIGRATOR_PASSWORD"
    yugabyte_password       = "YUGABYTE_PASSWORD"
    audit_operator_password = "AUDIT_OPERATOR_PASSWORD"
    audit_reader_password   = "AUDIT_READER_PASSWORD"
    audit_redactor_password = "AUDIT_REDACTOR_PASSWORD"
    audit_writer_password   = "AUDIT_WRITER_PASSWORD"
    signing_key             = "audit-signing.pem"
    seed_token              = "SEED_API_TOKEN"
    search_ca               = "search-ca.crt"
    search_password         = "OPENSEARCH_PASSWORD"
    search_cursor_key       = "OPENSEARCH_CURSOR_KEY"
  }
  search_secrets = {
    search_ca                = "ca.pem"
    search_certificate       = "node.pem"
    search_private_key       = "node-key.pem"
    search_admin_certificate = "admin.pem"
    search_admin_private_key = "admin-key.pem"
  }
  secret_files = merge(
    { for key, filename in local.data_secrets : "data/${key}" => { guest = "data", key = key, path = "${local.directory}/secrets/${filename}" } },
    { for key, filename in local.search_secrets : "search/${key}" => { guest = "search", key = key, path = "${local.directory}/search/${filename}" } },
    { "data/registry_config" = { guest = "data", key = "registry_config", path = "/root/.docker/config.json" } },
    { "data/ledger_ca"                 = { guest = "data", key = "ledger_ca", path = "${local.directory}/ledger/ca.crt" },
      "data/ledger_server_certificate" = { guest = "data", key = "ledger_server_certificate", path = "${local.directory}/ledger/node.yugabyte.crt" },
    "data/ledger_server_private_key" = { guest = "data", key = "ledger_server_private_key", path = "${local.directory}/ledger/node.yugabyte.key" } }
  )
  overlays = {
    "fdb-overlay/fdb.bash"       = "3b53cee50929e7d94fa24e97d3b39ae955c2840684a3b07ba0d31e4e228b98e8"
    "yugabyte-overlay/yugabyted" = "5f6cba4648db9f644a9335a853b9fd1726c615c4d5f722e1425419cb72238f5f"
  }
}
