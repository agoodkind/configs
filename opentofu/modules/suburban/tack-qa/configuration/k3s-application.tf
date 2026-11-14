locals {
  tack_k3s_application = {
    environment = {
      ENV                             = "production"
      PORT                            = "8000"
      FDB_TRANSACTION_TIMEOUT         = "5s"
      TACK_DATAGEN_ALLOW_TARGET       = "qa"
      OPENSEARCH_ENDPOINT             = "https://[3d06:bad:b01:210::5]:9200"
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
      AUDIT_VALID_SIGNERS             = "ed25519:12bdb0b77570f79e"
      AUDIT_KAFKA_BROKERS             = "[3d06:bad:b01:210::217]:29092"
    }
    ledger_hosts = {
      yb1 = "3d06:bad:b01:210::220"
      yb2 = "3d06:bad:b01:210::221"
      yb3 = "3d06:bad:b01:210::222"
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
