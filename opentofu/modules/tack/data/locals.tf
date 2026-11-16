locals {
  directory = "/opt/tack-qa"
  guests    = { data = var.data_guest }
  public = {
    fdb_cluster_contents = "tackqa:tackqa@[${var.data_guest.ipv6}]:4500"
    search_endpoint      = var.search_endpoint
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
  secret_files = merge(
    { for key, filename in local.data_secrets : "data/${key}" => { guest = "data", key = key, path = "${local.directory}/secrets/${filename}" } },
    { "data/registry_config" = { guest = "data", key = "registry_config", path = "/root/.docker/config.json" } },
    { "data/ledger_ca"                 = { guest = "data", key = "ledger_ca", path = "${local.directory}/ledger/ca.crt" },
      "data/ledger_server_certificate" = { guest = "data", key = "ledger_server_certificate", path = "${local.directory}/ledger/node.yugabyte.crt" },
    "data/ledger_server_private_key" = { guest = "data", key = "ledger_server_private_key", path = "${local.directory}/ledger/node.yugabyte.key" } }
  )
  overlays = var.overlays
}
