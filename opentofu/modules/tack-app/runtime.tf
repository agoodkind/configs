terraform {
  required_providers {
    docker = { source = "kreuzwerker/docker" }
  }
}

locals {
  image_repository   = "ghcr.io/agoodkind/tack-server"
  container_name     = "tack-app-1"
  compose_project    = "tack"
  compose_service    = "app"
  application_port   = 8000
  memory_mib         = 1024
  memory_swap_mib    = 2048
  stop_grace_seconds = 10
  log_directory      = "/var/log/tack"
  repair_directory   = "/var/lib/tack/repair"
  fdb_directory      = "/etc/foundationdb"
  search_ca_file     = "/etc/tack/search-ca.crt"
  ledger_ca_file     = "/etc/tack/ledger-certs/ca.crt"
  secret_source_dir  = "/var/lib/tack/secrets"
  secret_target_dir  = "/run/secrets"
  secret_names       = ["AUDIT_OPERATOR_DSN", "DATABASE_URL", "OPENSEARCH_CURSOR_KEY", "OPENSEARCH_PASSWORD"]

  command = ["serve", "--execute", "--operator-service", "tack-app"]

  # Tack ops deploy verify uses com.docker.compose.project and com.docker.compose.service to find app replicas. This container does not use Compose configuration labels because Compose does not manage it.
  labels = {
    "com.docker.compose.container-number" = "1"
    "com.docker.compose.oneoff"           = "False"
    "com.docker.compose.project"          = local.compose_project
    "com.docker.compose.service"          = local.compose_service
  }

  module_environment = merge(
    { for name in local.secret_names : "${name}_FILE" => "${local.secret_target_dir}/${name}" },
    {
      FDB_CLUSTER_FILE = "${local.fdb_directory}/fdb.cluster"
      OPENSEARCH_CA    = local.search_ca_file
      PORT             = tostring(local.application_port)
      TACK_REPAIR_DIR  = local.repair_directory
    },
  )
  environment = merge(var.environment, local.module_environment)

  writable_directories = toset([local.fdb_directory, local.repair_directory])

  read_only_files = merge(
    { for name in local.secret_names : "${local.secret_source_dir}/${name}" => "${local.secret_target_dir}/${name}" },
    {
      (local.search_ca_file) = local.search_ca_file
      (local.ledger_ca_file) = local.ledger_ca_file
    },
  )
}

resource "docker_image" "app" {
  name         = "${local.image_repository}:${var.commit}"
  keep_locally = true

  lifecycle {
    postcondition {
      condition     = self.image_id == var.server_digest
      error_message = "The image ${self.name} has ID ${self.image_id}. The image ID must equal the index digest ${var.server_digest}."
    }
  }
}

resource "docker_volume" "logs" {
  name   = var.log_volume.name
  driver = "local"

  dynamic "labels" {
    for_each = var.log_volume.labels
    content {
      label = labels.key
      value = labels.value
    }
  }

  lifecycle {
    prevent_destroy = true
  }
}

resource "docker_container" "app" {
  name    = local.container_name
  image   = docker_image.app.image_id
  command = local.command

  restart               = "unless-stopped"
  must_run              = true
  remove_volumes        = false
  destroy_grace_seconds = local.stop_grace_seconds
  memory                = local.memory_mib
  memory_swap           = local.memory_swap_mib

  env = [for name in sort(keys(local.environment)) : "${name}=${local.environment[name]}"]

  # With an empty host IP, Docker bound the Compose container's published 8000/tcp only to [::] on this network, which disables IPv4. The kreuzwerker/docker provider replaces an empty ports.ip with 0.0.0.0. This block sets ports.ip to :: to publish the port on the IPv6 wildcard address.
  ports {
    internal = local.application_port
    external = local.application_port
    ip       = "::"
    protocol = "tcp"
  }

  networks_advanced {
    name    = var.network.name
    aliases = [local.compose_service, local.container_name]
  }

  dynamic "host" {
    for_each = var.extra_hosts
    content {
      host = host.key
      ip   = host.value
    }
  }

  dynamic "labels" {
    for_each = local.labels
    content {
      label = labels.key
      value = labels.value
    }
  }

  dynamic "volumes" {
    for_each = local.writable_directories
    content {
      host_path      = volumes.value
      container_path = volumes.value
    }
  }

  volumes {
    volume_name    = docker_volume.logs.name
    container_path = local.log_directory
  }

  dynamic "mounts" {
    for_each = local.read_only_files
    content {
      type      = "bind"
      source    = mounts.key
      target    = mounts.value
      read_only = true
    }
  }

  lifecycle {
    precondition {
      condition     = length(setintersection(keys(var.environment), keys(local.module_environment))) == 0
      error_message = "The environment must not set variables that the module sets: ${join(", ", sort(keys(local.module_environment)))}."
    }

    precondition {
      condition     = !anytrue(values(var.compose_writer_active))
      error_message = "A Compose writer unit is active. Stop every Compose writer unit before creating the app container."
    }

    postcondition {
      condition = length(self.network_data) == 1 && alltrue([
        for attachment in self.network_data :
        attachment.network_name == var.network.name && attachment.ip_address == "" && attachment.ipv6_gateway == var.network.ipv6_gateway
      ])
      error_message = "The app container must have exactly one network attachment. Attach it to ${var.network.name} without an IPv4 address and with IPv6 gateway ${var.network.ipv6_gateway}."
    }
  }
}
