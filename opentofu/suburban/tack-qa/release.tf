locals {
  tack_release = module.configuration.release
  qa_role_credentials = {
    app            = { username = "tack_app", password = var.vault_tack_qa_app_password }
    migrator       = { username = "tack_migrator", password = var.vault_tack_qa_migrator_password }
    superuser      = { username = "yugabyte", password = var.vault_tack_qa_yugabyte_password }
    audit_operator = { username = "tack_audit_operator", password = var.vault_tack_qa_audit_operator_password }
    audit_reader   = { username = "tack_audit_reader", password = var.vault_tack_qa_audit_reader_password }
    audit_redactor = { username = "tack_audit_redactor", password = var.vault_tack_qa_audit_redactor_password }
    audit_writer   = { username = "tack_audit_writer", password = var.vault_tack_qa_audit_writer_password }
  }
  qa_database_urls = {
    for role, credentials in local.qa_role_credentials : role =>
    "postgres://${credentials.username}:${urlencode(credentials.password)}@yb1:5433/tack?sslmode=verify-full&sslrootcert=/etc/tack/ledger-certs/ca.crt"
  }
  qa_service_secrets = {
    database_url              = local.qa_database_urls.app
    migrator_database_url     = local.qa_database_urls.migrator
    superuser_database_url    = local.qa_database_urls.superuser
    audit_operator_dsn        = local.qa_database_urls.audit_operator
    audit_reader_dsn          = local.qa_database_urls.audit_reader
    audit_redactor_dsn        = local.qa_database_urls.audit_redactor
    audit_writer_dsn          = local.qa_database_urls.audit_writer
    app_password              = var.vault_tack_qa_app_password
    migrator_password         = var.vault_tack_qa_migrator_password
    yugabyte_password         = var.vault_tack_qa_yugabyte_password
    audit_operator_password   = var.vault_tack_qa_audit_operator_password
    audit_reader_password     = var.vault_tack_qa_audit_reader_password
    audit_redactor_password   = var.vault_tack_qa_audit_redactor_password
    audit_writer_password     = var.vault_tack_qa_audit_writer_password
    signing_key               = var.vault_tack_qa_audit_signing_key
    seed_token                = var.vault_tack_qa_seed_api_token
    ledger_ca                 = file("${path.module}/files/qa-ledger-ca.crt")
    ledger_server_certificate = var.vault_tack_qa_ledger_server_certificate
    ledger_server_private_key = var.vault_tack_qa_ledger_server_private_key
    registry_config = jsonencode({ auths = { "ghcr.io" = {
      auth = base64encode("${module.configuration.vm_recipe.registry_username}:${var.vault_tack_qa_ghcr_pull_token}")
    } } })
    search_ca                = var.vault_tack_qa_search_ca
    search_certificate       = var.vault_tack_search1_suburban_certificate
    search_private_key       = var.vault_tack_search1_suburban_private_key
    search_admin_certificate = var.vault_tack_qa_search_admin_certificate
    search_admin_private_key = var.vault_tack_qa_search_admin_private_key
    search_password          = var.vault_tack_qa_search_password
    search_password_hash     = var.vault_tack_qa_search_password_hash
    search_cursor_key        = var.vault_tack_qa_search_cursor_key
  }
}

module "tack_qa_services" {
  source              = "../../modules/tack/data"
  application_address = module.configuration.tack_k3s.guest.ipv6
  data_guest = merge(local.guests.tack_qa_suburban, {
    ipv6    = local.service_mapping.tack_qa_suburban.ipv6
    subnet  = local.service_mapping.tack_qa_suburban.docker_v6_subnet
    gateway = local.service_mapping.tack_qa_suburban.docker_v6_gateway
  })
  search_endpoint = module.tack_qa_search.endpoint
  search_ready    = module.tack_qa_search.ready
  store_images    = module.configuration.store_images
  overlays        = module.configuration.overlays
  search_username = module.configuration.tack_k3s_application.environment.OPENSEARCH_USERNAME
  search_settings = {
    replicas       = module.configuration.tack_k3s_application.environment.OPENSEARCH_REPLICAS
    shards         = module.configuration.tack_k3s_application.environment.OPENSEARCH_SHARDS
    routing_shards = module.configuration.tack_k3s_application.environment.OPENSEARCH_ROUTING_SHARDS
  }
  release_commit  = local.tack_release.commit
  server_image    = "ghcr.io/agoodkind/tack-server@${local.tack_release.server_digest}"
  consumer_image  = "ghcr.io/agoodkind/tack-audit-consumer@${local.tack_release.consumer_digest}"
  seed_email      = local.tack_release.accountable
  seed_name       = "QA Operator"
  signing_id      = var.tack_qa_signing_id
  secrets         = local.qa_service_secrets
  secret_versions = module.configuration.service_secret_versions
  depends_on      = [module.docker_guest]
}

module "tack_qa_search" {
  source = "../../modules/tack/search"
  search_guest = merge(local.guests.tack_search1_suburban, {
    ipv6 = local.service_mapping.tack_search1_suburban.ipv6
  })
  runtime_identity = module.configuration.search_runtime_identity
  image            = module.configuration.store_images.opensearch
  endpoint         = module.configuration.tack_k3s_application.environment.OPENSEARCH_ENDPOINT
  search_username  = module.configuration.tack_k3s_application.environment.OPENSEARCH_USERNAME
  search_admin_dn  = var.tack_qa_search_admin_dn
  search_node_dn   = var.tack_qa_search_node_dn
  secrets          = local.qa_service_secrets
  secret_versions  = module.configuration.service_secret_versions
  depends_on       = [module.docker_guest]
}
