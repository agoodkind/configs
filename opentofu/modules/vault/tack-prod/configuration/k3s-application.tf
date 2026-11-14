locals {
  tack_k3s_application = {
    environment = {
      ENV                             = "production"
      PORT                            = "8000"
      FDB_TRANSACTION_TIMEOUT         = "5s"
      TACK_DATAGEN_ALLOW_TARGET       = ""
      OPENSEARCH_ENDPOINT             = "https://[3d06:bad:b01::254]:9200"
      OPENSEARCH_USERNAME             = "tack_runtime"
      OPENSEARCH_PUBLIC_ENABLED       = "true"
      OPENSEARCH_MODEL_REPAIR_ENABLED = "false"
      OPENSEARCH_SHARDS               = "1"
      OPENSEARCH_ROUTING_SHARDS       = "8"
      OPENSEARCH_REPLICAS             = "0"
      OPENSEARCH_SESSION_IDLE_TIMEOUT = "2m"
      LOG_LEVEL                       = "debug"
      LOG_MAX_BACKUPS                 = "0"
      LOG_MAX_AGE_DAYS                = "0"
      LOG_MAX_SIZE_MB                 = "100"
      AUDIT_VALID_SIGNERS             = "ed25519:f04a5d764fecb815"
      AUDIT_KAFKA_BROKERS             = "[3d06:bad:b01::117]:29092"
    }
    ledger_hosts = {
      yb1 = "3d06:bad:b01::120"
      yb2 = "3d06:bad:b01::121"
      yb3 = "3d06:bad:b01::122"
    }
    secret_versions = {
      database_url       = 1
      audit_operator_dsn = 1
      search_password    = 1
      search_cursor_key  = 1
      search_ca          = 1
      fdb_bootstrap      = 1
    }
    registry_credentials_version = 1
  }
}

output "tack_k3s_application" {
  value = local.tack_k3s_application
}
