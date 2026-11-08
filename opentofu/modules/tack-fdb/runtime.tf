terraform {
  required_providers {
    docker   = { source = "kreuzwerker/docker" }
    pveguest = { source = "tofu.home.arpa/agoodkind/pveguest" }
  }
}

locals {
  image_repository        = "foundationdb/foundationdb"
  compose_project         = "tack"
  server_service          = "fdb"
  agent_service           = "fdb-backup-agent"
  server_container_name   = "tack-fdb-1"
  agent_container_name    = "tack-fdb-backup-agent-1"
  server_port             = 4500
  server_memory_mib       = 2048
  server_memory_swap_mib  = 4096
  agent_memory_mib        = 512
  agent_memory_swap_mib   = 1024
  stop_grace_seconds      = 10
  cluster_directory       = "/etc/foundationdb"
  cluster_file            = "${local.cluster_directory}/fdb.cluster"
  state_directory         = "/var/fdb"
  data_directory          = "${local.state_directory}/data"
  overlay_target          = "${local.state_directory}/scripts/fdb.bash"
  overlay_mode            = "0755"
  overlay_repository_url  = "https://api.github.com/repos/agoodkind/tack"
  overlay_repository_path = "fdb-overlay/fdb.bash"
  overlay_media_type      = "application/vnd.github.raw"
  blobstore_host          = "fdb-blobstore-host"
  agent_entrypoint        = ["/usr/bin/backup_agent"]
  agent_command           = ["-C", local.cluster_file]

  # The overlay writes this invalid connection string if the cluster file is missing or empty.
  # FoundationDB then fails to start instead of starting a new cluster.
  # The overlay keeps an existing nonempty cluster file.
  missing_cluster_file_contents = "cluster-file-required"

  health = {
    test                 = ["CMD-SHELL", "fdbcli -C ${local.cluster_file} --exec 'status minimal' 2>&1 | grep -q 'available'"]
    interval_seconds     = 10
    timeout_seconds      = 10
    start_period_seconds = 30
    retries              = 10
  }
  health_wait_seconds = local.health.start_period_seconds + local.health.retries * local.health.interval_seconds

  server_environment = {
    FDB_CLUSTER_FILE          = local.cluster_file
    FDB_CLUSTER_FILE_CONTENTS = local.missing_cluster_file_contents
    FDB_MACHINE_ID            = var.locality_id
    FDB_NETWORKING_MODE       = "container"
    FDB_PORT                  = tostring(local.server_port)
    FDB_PROCESS_CLASS         = "unset"
    FDB_PUBLIC_ADDRESS        = var.public_address
    FDB_ZONE_ID               = var.locality_id
  }

  # The backup alarm uses docker compose restart fdb-backup-agent, which finds the container
  # through its Compose project and service labels. Compose does not manage these containers.
  container_labels = {
    "com.docker.compose.container-number" = "1"
    "com.docker.compose.oneoff"           = "False"
    "com.docker.compose.project"          = local.compose_project
  }
}

# The server container mounts this overlay as its startup script.
# The mount references this resource to create or adopt the file before creating the container.
resource "pveguest_download" "overlay" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  url    = "${local.overlay_repository_url}/contents/${local.overlay_repository_path}?ref=${var.tack_commit}"
  sha256 = var.overlay.sha256
  path   = var.overlay.path
  mode   = local.overlay_mode

  request_headers_wo = {
    Accept        = local.overlay_media_type
    Authorization = "Bearer ${var.repository_read_token}"
  }
  request_headers_wo_version = var.repository_read_token_version
}

resource "docker_image" "fdb" {
  name         = "${local.image_repository}:${var.image.version}"
  keep_locally = true

  lifecycle {
    postcondition {
      condition     = self.image_id == var.image.id
      error_message = "The image ${self.name} has ID ${self.image_id}. The image ID must equal ${var.image.id}."
    }
  }
}

resource "docker_volume" "fdb" {
  for_each = var.volumes

  name   = each.value.name
  driver = "local"

  dynamic "labels" {
    for_each = each.value.labels
    content {
      label = labels.key
      value = labels.value
    }
  }

  lifecycle {
    prevent_destroy = true
  }
}

resource "docker_container" "server" {
  name         = local.server_container_name
  image        = docker_image.fdb.image_id
  network_mode = "host"

  restart               = "unless-stopped"
  must_run              = true
  remove_volumes        = false
  destroy_grace_seconds = local.stop_grace_seconds
  memory                = local.server_memory_mib
  memory_swap           = local.server_memory_swap_mib

  wait         = true
  wait_timeout = local.health_wait_seconds

  env = [for name in sort(keys(local.server_environment)) : "${name}=${local.server_environment[name]}"]

  healthcheck {
    test         = local.health.test
    interval     = "${local.health.interval_seconds}s"
    timeout      = "${local.health.timeout_seconds}s"
    start_period = "${local.health.start_period_seconds}s"
    retries      = local.health.retries
  }

  dynamic "labels" {
    for_each = merge(local.container_labels, { "com.docker.compose.service" = local.server_service })
    content {
      label = labels.key
      value = labels.value
    }
  }

  volumes {
    volume_name    = docker_volume.fdb["state"].name
    container_path = local.state_directory
  }

  # Mount the existing data volume by name. The Docker provider does not reuse the anonymous
  # volume when it replaces the container, and a new volume would not contain the data files.
  volumes {
    volume_name    = docker_volume.fdb["data"].name
    container_path = local.data_directory
  }

  volumes {
    host_path      = local.cluster_directory
    container_path = local.cluster_directory
  }

  mounts {
    type      = "bind"
    source    = pveguest_download.overlay.path
    target    = local.overlay_target
    read_only = true
  }
}

resource "docker_container" "backup_agent" {
  name       = local.agent_container_name
  image      = docker_image.fdb.image_id
  entrypoint = local.agent_entrypoint
  command    = local.agent_command

  restart               = "unless-stopped"
  must_run              = true
  remove_volumes        = false
  destroy_grace_seconds = local.stop_grace_seconds
  memory                = local.agent_memory_mib
  memory_swap           = local.agent_memory_swap_mib

  networks_advanced {
    name    = var.network.name
    aliases = [local.agent_service, local.agent_container_name]
  }

  host {
    host = local.blobstore_host
    ip   = var.blobstore_address
  }

  dynamic "labels" {
    for_each = merge(local.container_labels, { "com.docker.compose.service" = local.agent_service })
    content {
      label = labels.key
      value = labels.value
    }
  }

  volumes {
    host_path      = local.cluster_directory
    container_path = local.cluster_directory
  }

  # Mount the existing agent data volume by name to avoid creating another anonymous volume
  # when the Docker provider replaces the container.
  volumes {
    volume_name    = docker_volume.fdb["agent_data"].name
    container_path = local.data_directory
  }

  lifecycle {
    postcondition {
      condition = length(self.network_data) == 1 && alltrue([
        for attachment in self.network_data :
        attachment.network_name == var.network.name && attachment.ip_address == "" && attachment.ipv6_gateway == var.network.ipv6_gateway
      ])
      error_message = "The backup agent must have exactly one network attachment. Attach it to ${var.network.name} without an IPv4 address and with IPv6 gateway ${var.network.ipv6_gateway}."
    }
  }
}
