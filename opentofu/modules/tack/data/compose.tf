locals {
  data_network_name = "data_default"
  operator_environment = {
    ENV                                  = "production"
    DATABASE_URL_FILE                    = "/run/secrets/DATABASE_URL"
    AUDIT_OPERATOR_DSN_FILE              = "/run/secrets/AUDIT_OPERATOR_DSN"
    AUDIT_READER_DSN_FILE                = "/run/secrets/AUDIT_READER_DSN"
    AUDIT_REDACTOR_DSN_FILE              = "/run/secrets/AUDIT_REDACTOR_DSN"
    FDB_CLUSTER_FILE                     = "/etc/foundationdb/fdb.cluster"
    AUDIT_KAFKA_BROKERS                  = local.public.audit_kafka_brokers
    AUDIT_VALID_SIGNERS                  = var.signing_id
    OPENSEARCH_ENDPOINT                  = local.public.search_endpoint
    OPENSEARCH_USERNAME                  = var.search_username
    OPENSEARCH_MAINTENANCE_USERNAME      = var.search_username
    OPENSEARCH_PASSWORD_FILE             = "/run/secrets/OPENSEARCH_PASSWORD"
    OPENSEARCH_MAINTENANCE_PASSWORD_FILE = "/run/secrets/OPENSEARCH_PASSWORD"
    OPENSEARCH_CA                        = "/run/secrets/search-ca.crt"
    OPENSEARCH_CURSOR_KEY_FILE           = "/run/secrets/OPENSEARCH_CURSOR_KEY"
    OPENSEARCH_REPLICAS                  = var.search_settings.replicas
    OPENSEARCH_SHARDS                    = var.search_settings.shards
    OPENSEARCH_ROUTING_SHARDS            = var.search_settings.routing_shards
    SEED_EMAIL                           = var.seed_email
    SEED_NAME                            = var.seed_name
    SEED_API_TOKEN_FILE                  = "/run/secrets/SEED_API_TOKEN"
    TACK_APP_PASSWORD_FILE               = "/run/secrets/TACK_APP_PASSWORD"
    TACK_MIGRATOR_PASSWORD_FILE          = "/run/secrets/TACK_MIGRATOR_PASSWORD"
    AUDIT_OPERATOR_PASSWORD_FILE         = "/run/secrets/AUDIT_OPERATOR_PASSWORD"
    AUDIT_READER_PASSWORD_FILE           = "/run/secrets/AUDIT_READER_PASSWORD"
    AUDIT_REDACTOR_PASSWORD_FILE         = "/run/secrets/AUDIT_REDACTOR_PASSWORD"
    AUDIT_WRITER_PASSWORD_FILE           = "/run/secrets/AUDIT_WRITER_PASSWORD"
  }
  operator = {
    image       = var.server_image
    profiles    = ["bootstrap"]
    extra_hosts = { yb1 = var.data_guest.ipv6 }
    environment = local.operator_environment
    volumes     = ["/opt/tack-qa/secrets:/run/secrets:ro", "/etc/foundationdb:/etc/foundationdb", "/opt/tack-qa/ledger:/etc/tack/ledger-certs:ro"]
  }
  data_compose = {
    services = {
      fdb = {
        image     = var.store_images.foundationdb
        restart   = "unless-stopped"
        mem_limit = "768m"
        environment = {
          FDB_NETWORKING_MODE       = "container", FDB_PORT = "4500", FDB_PROCESS_CLASS = "unset"
          FDB_CLUSTER_FILE          = "/etc/foundationdb/fdb.cluster", FDB_PUBLIC_ADDRESS = var.data_guest.ipv6
          FDB_CLUSTER_FILE_CONTENTS = local.public.fdb_cluster_contents
          FDB_ZONE_ID               = "tack-qa-data", FDB_MACHINE_ID = "tack-qa-data"
        }
        network_mode = "host"
        volumes      = ["fdb:/var/fdb", "/etc/foundationdb:/etc/foundationdb", "/opt/tack-qa/fdb-overlay/fdb.bash:/var/fdb/scripts/fdb.bash:ro"]
      }
      yugabyte = {
        image        = var.store_images.yugabyte
        extra_hosts  = { yugabyte = var.data_guest.ipv6 }
        restart      = "unless-stopped"
        mem_limit    = "3g"
        command      = ["/home/yugabyte/bin/yugabyted", "start", "--daemon=false", "--base_dir=/home/yugabyte/var", "--advertise_address=yugabyte", "--listen=yugabyte", "--master_flags=replication_factor=1,certs_dir=/etc/tack/ledger-certs,use_node_to_node_encryption=true,use_client_to_server_encryption=true,allow_insecure_connections=false", "--tserver_flags=flagfile=/home/yugabyte/tserver.flags"]
        network_mode = "host"
        healthcheck  = { test = ["CMD", "/usr/bin/env", "bash", "/opt/tack/healthcheck.sh"], interval = "10s", timeout = "5s", retries = 24, start_period = "30s" }
        volumes      = ["yugabyte:/home/yugabyte/var", "/opt/tack-qa/secrets/YUGABYTE_PASSWORD:/run/secrets/YUGABYTE_PASSWORD:ro", "/opt/tack-qa/yugabyte-overlay/yugabyted:/home/yugabyte/bin/yugabyted:ro", "/opt/tack-qa/data/yugabyte-bootstrap.sh:/opt/tack/bootstrap.sh:ro", "/opt/tack-qa/data/yugabyte-ready.sql:/opt/tack/ready.sql:ro", "/opt/tack-qa/data/yugabyte-database.sql:/opt/tack/database.sql:ro", "/opt/tack-qa/data/yugabyte-password.sql:/opt/tack/password.sql:ro", "/opt/tack-qa/data/yugabyte-healthcheck.sh:/opt/tack/healthcheck.sh:ro", "/opt/tack-qa/data/tserver.flags:/home/yugabyte/tserver.flags:ro", "/opt/tack-qa/ledger:/etc/tack/ledger-certs:ro"]
      }
      kafka = {
        image     = var.store_images.kafka
        restart   = "unless-stopped"
        mem_limit = "768m"
        environment = {
          KAFKA_NODE_ID                                  = "1", KAFKA_PROCESS_ROLES = "broker,controller"
          KAFKA_LISTENERS                                = "PLAINTEXT://[::]:9092,CONTROLLER://[::1]:9093"
          KAFKA_ADVERTISED_LISTENERS                     = "PLAINTEXT://[${var.data_guest.ipv6}]:9092"
          KAFKA_CONTROLLER_QUORUM_VOTERS                 = "1@[::1]:9093"
          KAFKA_LISTENER_SECURITY_PROTOCOL_MAP           = "CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT"
          KAFKA_INTER_BROKER_LISTENER_NAME               = "PLAINTEXT", KAFKA_CONTROLLER_LISTENER_NAMES = "CONTROLLER"
          KAFKA_LOG_DIRS                                 = "/var/lib/kafka/data", KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR = "1"
          KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR = "1", KAFKA_TRANSACTION_STATE_LOG_MIN_ISR = "1"
          KAFKA_DEFAULT_REPLICATION_FACTOR               = "1", KAFKA_MIN_INSYNC_REPLICAS = "1", KAFKA_NUM_PARTITIONS = "256"
          KAFKA_HEAP_OPTS                                = "-Xms256m -Xmx384m", CLUSTER_ID = "dGFjay1xYS1mcmVzaC0wMQ"
        }
        network_mode = "host"
        volumes      = ["kafka:/var/lib/kafka/data"]
      }
      audit-consumer = {
        network_mode = "host"
        image        = var.consumer_image
        restart      = "unless-stopped"
        mem_limit    = "512m"
        extra_hosts  = { yb1 = var.data_guest.ipv6 }
        environment = {
          ENV                              = "production", AUDIT_CONSUMER_KAFKA_BROKERS = local.public.audit_kafka_brokers
          AUDIT_CONSUMER_YUGABYTE_DSN_FILE = "/run/secrets/AUDIT_WRITER_DSN"
          AUDIT_CONSUMER_SIGNING_KEY_PATH  = "/run/secrets/audit-signing.pem"
          AUDIT_CONSUMER_SIGNING_HOST      = "tack-qa-data", FDB_CLUSTER_FILE = "/etc/foundationdb/fdb.cluster"
        }
        volumes = local.operator.volumes
      }
      operator = local.operator
      ledger-bootstrap = merge(local.operator, {
        volumes = concat(local.operator.volumes, ["/var/run/docker.sock:/var/run/docker.sock"])
        environment = merge(local.operator_environment, {
          DATABASE_URL_FILE            = "/run/secrets/SUPERUSER_DATABASE_URL"
          AUDIT_KAFKA_BROKERS          = ""
          SEED_EMAIL                   = ""
          SEED_NAME                    = ""
          TACK_BACKUP_FDB_CONTINUOUS   = "false"
          TACK_OPS_FDB_REDUNDANCY_MODE = "single"
          TACK_OPS_FDB_CLUSTER_DIR     = "/etc/foundationdb"
          TACK_BACKUP_FDB_NETWORK      = local.data_network_name
          TACK_BACKUP_FDB_IMAGE        = var.store_images.foundationdb
        })
      })
    }
    volumes = { fdb = {}, yugabyte = {}, kafka = {} }
    networks = { default = {
      name        = local.data_network_name
      enable_ipv6 = true, enable_ipv4 = false
      driver_opts = { "com.docker.network.bridge.gateway_mode_ipv6" = "routed" }
      ipam        = { config = [{ subnet = var.data_guest.subnet, gateway = var.data_guest.gateway }] }
    } }
  }
}
