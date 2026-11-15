locals {
  tack_k3s_application = {
    environment = {
      ENV                             = "production"
      PORT                            = "8000"
      FDB_TRANSACTION_TIMEOUT         = "5s"
      TACK_DATAGEN_ALLOW_TARGET       = "qa"
      OPENSEARCH_ENDPOINT             = "https://[3d06:bad:b01:210::227]:9200"
      OPENSEARCH_USERNAME             = "tack_runtime"
      OPENSEARCH_PUBLIC_ENABLED       = "true"
      OPENSEARCH_MODEL_REPAIR_ENABLED = "true"
      OPENSEARCH_SHARDS               = "1"
      OPENSEARCH_ROUTING_SHARDS       = "8"
      OPENSEARCH_REPLICAS             = "0"
      OPENSEARCH_SESSION_IDLE_TIMEOUT = "2m"
      LOG_LEVEL                       = "debug"
      LOG_MAX_BACKUPS                 = "0"
      LOG_MAX_AGE_DAYS                = "0"
      LOG_MAX_SIZE_MB                 = "100"
      AUDIT_KAFKA_BROKERS             = "[3d06:bad:b01:210::217]:9092"
    }
    ledger_hosts = {
      yb1 = "3d06:bad:b01:210::217"
    }
    secret_versions = {
      database_url       = 2
      audit_operator_dsn = 2
      search_password    = 2
      search_cursor_key  = 1
      search_ca          = 1
      fdb_bootstrap      = 2
    }
    registry_credentials_version = 1
  }
}

output "tack_k3s_application" {
  value = local.tack_k3s_application
}
