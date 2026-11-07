locals {
  ledger_guests = {
    yb1 = "tack_data1_suburban"
    yb2 = "tack_data2_suburban"
    yb3 = "tack_data3_suburban"
  }

  tack_app = {
    network = {
      name         = "tack_default"
      ipv6_gateway = local.service_mapping.tack_app2_suburban.docker_v6_gateway
    }

    log_volume = {
      name = "tack_tack-logs"
      labels = {
        "com.docker.compose.config-hash" = "c09b8bd547b92433a7f3079bd3582ffc6c963b176d6360f4f6edb8e5949a7e74"
        "com.docker.compose.project"     = "tack"
        "com.docker.compose.version"     = "5.5.1"
        "com.docker.compose.volume"      = "tack-logs"
      }
    }

    extra_hosts = { for name, guest in local.ledger_guests : name => local.service_mapping[guest].ipv6 }

    environment = {
      AUDIT_KAFKA_BROKERS                 = "[${local.service_mapping.tack_qa_suburban.ipv6}]:9094"
      AUDIT_KAFKA_CLIENT_ID               = "tack-audit-producer"
      AUDIT_KAFKA_PRODUCE_TIMEOUT         = "15s"
      AUDIT_KAFKA_TOPIC                   = "audit.events.v1"
      AUDIT_VALID_SIGNERS                 = "ed25519:12bdb0b77570f79e"
      ENV                                 = "production"
      FDB_TRANSACTION_TIMEOUT             = "5s"
      LOG_COMMAND_DIR                     = "/var/log/tack/commands"
      LOG_JSON_FILE                       = "/var/log/tack/app.jsonl"
      LOG_LEVEL                           = "debug"
      LOG_MAX_AGE_DAYS                    = "0"
      LOG_MAX_BACKUPS                     = "0"
      LOG_MAX_SIZE_MB                     = "100"
      LOG_TEXT_FILE                       = "/var/log/tack/app.log"
      OPENSEARCH_ENDPOINT                 = "https://[3d06:bad:b01:210::5]:9200"
      OPENSEARCH_MAX_INDEX_BYTES          = ""
      OPENSEARCH_MAX_RETIRED_PAGES        = ""
      OPENSEARCH_MAX_RETIREMENT_AGE       = ""
      OPENSEARCH_MAX_RETRIES              = ""
      OPENSEARCH_MODEL_REPAIR_ENABLED     = "true"
      OPENSEARCH_PAGE_BYTES               = ""
      OPENSEARCH_PUBLIC_ENABLED           = "true"
      OPENSEARCH_QUERY_ADMISSION_WAIT     = ""
      OPENSEARCH_QUERY_BATCH_SIZE         = ""
      OPENSEARCH_QUERY_CONCURRENCY        = ""
      OPENSEARCH_QUERY_DEADLINE           = ""
      OPENSEARCH_QUERY_MAX_BATCHES        = ""
      OPENSEARCH_QUERY_MAX_BYTES          = ""
      OPENSEARCH_QUERY_MAX_RESULTS        = ""
      OPENSEARCH_REPLICAS                 = "0"
      OPENSEARCH_REQUEST_TIMEOUT          = ""
      OPENSEARCH_RESPONSE_MAX_BYTES       = ""
      OPENSEARCH_RETENTION_CHECK_INTERVAL = ""
      OPENSEARCH_ROUTING_SHARDS           = "8"
      OPENSEARCH_SESSION_ABSOLUTE_TIMEOUT = ""
      OPENSEARCH_SESSION_IDLE_TIMEOUT     = "2m"
      OPENSEARCH_SESSION_TOKEN_MAX_BYTES  = ""
      OPENSEARCH_SHARDS                   = "1"
      OPENSEARCH_USERNAME                 = "tack_runtime"
      OPENSEARCH_WORKER_CONCURRENCY       = ""
      OPENSEARCH_WORKER_IDLE_INTERVAL     = ""
      OPENSEARCH_WORKER_LEASE             = ""
      OPENSEARCH_WORKER_MAX_BYTES         = ""
      OPENSEARCH_WORKER_MAX_PAGES         = ""
      OPENSEARCH_WORKER_OPERATION_TIMEOUT = ""
      OPENSEARCH_WORKER_SLICE_BUDGET      = ""
      OPENSEARCH_WORK_CLASS_WEIGHTS       = ""
      TACK_DATAGEN_ALLOW_TARGET           = "qa"
    }
  }
}

output "tack_app" { value = local.tack_app }
