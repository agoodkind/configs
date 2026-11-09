# This module updates the Tack app and audit-consumer images on one guest and runs audited checks. With writer = compose, the pull and up units pull the release images and recreate the services with docker compose. With writer = docker, the module stops those units, deletes their unit files, and runs tack-release-ready.service and the verify unit after each change to runtime_revision. The Docker provider in another module creates the app container. Each systemd unit is a oneshot, and pveguest_systemd_unit restarts it when its unit file changes. The module does not change the Compose file, overrides, migrations, or credentials. A release that changes any of them needs another change. The provider limits each systemctl command to 300 seconds, and each unit stops after 280 seconds.

terraform {
  required_providers {
    pveguest = { source = "tofu.home.arpa/agoodkind/pveguest" }
  }
}

variable "node" {
  description = "Provider node key of the guest."
  type        = string
}

variable "vmid" {
  description = "Proxmox ID of the guest."
  type        = number
}

variable "kind" {
  description = "Guest kind."
  type        = string
}

variable "role" {
  description = "Role of the Tack guest. The `owner` role runs `app` and `audit-consumer` and performs the search and audit queue checks. The `app` role runs only the `app` service."
  type        = string

  validation {
    condition     = contains(["owner", "app"], var.role)
    error_message = "The role must be `owner` or `app`."
  }
}

variable "commit" {
  description = "Tack commit with 40 characters used as the image tag."
  type        = string

  validation {
    condition     = can(regex("^[0-9a-f]{40}$", var.commit))
    error_message = "Commit must contain exactly 40 lowercase hexadecimal characters."
  }
}

variable "server_digest" {
  description = "Expected tack-server index digest."
  type        = string

  validation {
    condition     = can(regex("^sha256:[0-9a-f]{64}$", var.server_digest))
    error_message = "Digest must start with `sha256:` followed by exactly 64 lowercase hexadecimal characters."
  }
}

variable "consumer_digest" {
  description = "Expected tack-audit-consumer index digest. Set this value only for the owner role."
  type        = string
  default     = null

  validation {
    condition     = (var.role == "owner") == (var.consumer_digest != null)
    error_message = "Set consumer_digest for the owner role and use null for the app role."
  }

  validation {
    condition     = var.consumer_digest == null || can(regex("^sha256:[0-9a-f]{64}$", var.consumer_digest))
    error_message = "Digest must start with `sha256:` followed by exactly 64 lowercase hexadecimal characters."
  }
}

variable "accountable_email" {
  description = "Email address of the person accountable for the release."
  type        = string
}

variable "writer" {
  description = "Choose the app container writer. The default, `compose`, uses the pull and up units. `docker` stops those units and deletes their unit files, and the Docker provider in tack-app creates the container. Each change to runtime_revision restarts the health check and verify units."
  type        = string
  default     = "compose"

  validation {
    condition     = contains(["compose", "docker"], var.writer)
    error_message = "The writer must be `compose` or `docker`."
  }

  validation {
    condition     = var.writer == "compose" || var.role == "app"
    error_message = "Use the `docker` writer only with the `app` role."
  }
}

variable "runtime_revision" {
  description = "Set the app container ID for the `docker` writer and use null for the `compose` writer. A change to this value restarts the health check and verify units."
  type        = string
  default     = null

  validation {
    condition     = (var.writer == "docker") == (var.runtime_revision != null)
    error_message = "Set runtime_revision for the `docker` writer and use null for the `compose` writer."
  }
}

locals {
  install_dir        = "/root/tack"
  docker             = "/usr/bin/docker compose"
  compose_writer     = var.writer == "compose"
  health_check       = "/usr/bin/curl --fail --silent --show-error --max-time 5 --retry 40 --retry-delay 5 --retry-all-errors --retry-max-time 200 http://[::1]:8000/healthz"
  is_owner           = var.role == "owner"
  services           = local.is_owner ? "app audit-consumer" : "app"
  operator_namespace = "0a6f7572-cafe-dead-beef-000000000004"
  operator_email     = lower(var.accountable_email)
  operator_id        = uuidv5(local.operator_namespace, local.operator_email)
  identity           = "--operator-service tack-release --operator-session ${var.commit} --operator-id ${local.operator_id} --operator-email ${local.operator_email} --deploy-commit ${var.commit}"
  verify_scope       = local.is_owner ? "--tack-audit-consumer-digest ${var.consumer_digest}" : "--service app"
  audit_reader_files = "-f docker-compose.yml -f docker-compose.override.yml -f docker-compose.audit-reader.yml"

  compose_units = {
    "tack-release-pull.service" = {
      description = "Pull Tack release images"
      lines = [
        "Environment=TACK_IMAGE_TAG=${var.commit}",
        "ExecStart=${local.docker} pull ${local.services}",
      ]
    }
    "tack-release-up.service" = {
      description = "Record the image tag and recreate Tack services"
      lines = [
        "ExecStartPre=/usr/bin/grep -q ^TACK_IMAGE_TAG= .env",
        "ExecStartPre=/usr/bin/sed -i -E s/^TACK_IMAGE_TAG=.*/TACK_IMAGE_TAG=${var.commit}/ .env",
        "ExecStart=${local.docker} up -d --wait --wait-timeout 60 --no-deps --no-build ${local.services}",
        "ExecStartPost=${local.health_check}",
      ]
    }
  }

  docker_units = {
    "tack-release-ready.service" = {
      description = "Check the Tack app health endpoint"
      lines = [
        "ExecStart=${local.health_check}",
      ]
    }
  }

  # The verify unit sets TACK_IMAGE_TAG to select the release image for tack-ops. This environment variable takes precedence over /root/tack/.env, which the docker writer does not update.
  verify_lines = concat(
    local.compose_writer ? [] : ["Environment=TACK_IMAGE_TAG=${var.commit}"],
    ["ExecStart=${local.docker} run --rm tack-ops ops deploy verify --execute --tag ${var.commit} --tack-server-digest ${var.server_digest} ${local.verify_scope} ${local.identity}"],
  )

  stage_units = merge(local.compose_writer ? local.compose_units : local.docker_units, {
    "tack-release-verify.service" = {
      description = "Verify that the Tack containers use the release images"
      lines       = local.verify_lines
    }
  })

  docker_units_enabled = local.compose_writer ? {} : local.docker_units
  runtime_restart      = local.compose_writer ? {} : { runtime = var.runtime_revision }

  check_units = local.is_owner ? {
    "tack-release-search-verify.service" = {
      description = "Run ops search verify after the release"
      lines = [
        "ExecStart=${local.docker} run --rm tack-ops ops search verify --execute ${local.identity}",
      ]
    }
    "tack-release-dlq-inspect.service" = {
      description = "Run ops audit dlq inspect after the release"
      lines = [
        "ExecStart=${local.docker} ${local.audit_reader_files} run --rm audit-ops ops audit dlq inspect --execute ${local.identity}",
      ]
    }
  } : {}

  units = merge(local.stage_units, local.check_units)
}

resource "pveguest_file" "unit" {
  for_each = local.units

  node = var.node
  vmid = var.vmid
  kind = var.kind

  path = "/etc/systemd/system/${each.key}"
  content = join("\n", concat([
    "[Unit]",
    "Description=${each.value.description}",
    "After=docker.service",
    "",
    "[Service]",
    "Type=oneshot",
    "RemainAfterExit=yes",
    "WorkingDirectory=${local.install_dir}",
    "TimeoutStartSec=280",
  ], each.value.lines, [""]))
}

# With the docker writer, these resources stop the pull and up units before OpenTofu deletes their unit files. allow_missing accepts the missing units on later plans and these resources keep them stopped.
resource "pveguest_systemd_unit" "pull" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  name          = "tack-release-pull.service"
  enabled       = false
  active        = local.compose_writer
  allow_missing = !local.compose_writer

  restart_on = local.compose_writer ? { unit = pveguest_file.unit["tack-release-pull.service"].write_id } : null
}

resource "pveguest_systemd_unit" "up" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  name          = "tack-release-up.service"
  enabled       = false
  active        = local.compose_writer
  allow_missing = !local.compose_writer

  restart_on = local.compose_writer ? { unit = pveguest_file.unit["tack-release-up.service"].write_id } : null

  depends_on = [pveguest_systemd_unit.pull]
}

resource "pveguest_systemd_unit" "ready" {
  for_each = local.docker_units_enabled

  node = var.node
  vmid = var.vmid
  kind = var.kind

  name    = each.key
  enabled = false
  active  = true

  restart_on = merge({ unit = pveguest_file.unit[each.key].write_id }, local.runtime_restart)

  depends_on = [pveguest_systemd_unit.up]
}

resource "pveguest_systemd_unit" "verify" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  name    = "tack-release-verify.service"
  enabled = false
  active  = true

  restart_on = merge({ unit = pveguest_file.unit["tack-release-verify.service"].write_id }, local.runtime_restart)

  depends_on = [pveguest_systemd_unit.up, pveguest_systemd_unit.ready]
}

resource "pveguest_systemd_unit" "check" {
  for_each = local.check_units

  node = var.node
  vmid = var.vmid
  kind = var.kind

  name    = each.key
  enabled = false
  active  = true

  restart_on = { unit = pveguest_file.unit[each.key].write_id }

  depends_on = [pveguest_systemd_unit.verify]
}

output "compose_writer_active" {
  description = "Map of Compose pull and up unit names to their active states. The tack-app module checks these states before creating the container."
  value = {
    "tack-release-pull.service" = pveguest_systemd_unit.pull.active
    "tack-release-up.service"   = pveguest_systemd_unit.up.active
  }
}
