terraform {
  required_providers {
    pveguest = { source = "tofu.home.arpa/agoodkind/pveguest", version = ">= 0.1.3" }
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

variable "commit" {
  description = "Tack commit with 40 characters used as the image tag."
  type        = string

  validation {
    condition     = can(regex("^[0-9a-f]{40}$", var.commit))
    error_message = "Commit must contain exactly 40 lowercase hexadecimal characters."
  }
}

variable "accountable_email" {
  description = "Email address of the person accountable for the release."
  type        = string
}

variable "configuration_revisions" {
  description = "Changes to these nonsecret Compose configuration values trigger a service update and release checks."
  type        = map(string)
  default     = {}
}

variable "additional_compose_files" {
  description = "Compose files appended after the existing base and override files."
  type        = list(string)
  default     = []
}

locals {
  install_dir         = "/root/tack"
  compose_files       = concat(["docker-compose.yml", "docker-compose.override.yml"], var.additional_compose_files)
  docker              = join(" ", concat(["/usr/bin/docker compose"], [for path in local.compose_files : "-f ${path}"]))
  operator_namespace  = "0a6f7572-cafe-dead-beef-000000000004"
  operator_email      = lower(var.accountable_email)
  operator_id         = uuidv5(local.operator_namespace, local.operator_email)
  identity            = "--operator-service tack-release --operator-session ${var.commit} --operator-id ${local.operator_id} --operator-email ${local.operator_email} --deploy-commit ${var.commit}"
  audit_reader_docker = join(" ", concat(["/usr/bin/docker compose"], [for path in concat(["docker-compose.yml", "docker-compose.override.yml", "docker-compose.audit-reader.yml"], var.additional_compose_files) : "-f ${path}"]))

  stage_units = {
    "tack-release-pull.service" = {
      description = "Pull Tack audit consumer and operator images"
      lines = [
        "ExecStart=${local.docker} pull audit-consumer tack-ops audit-ops",
      ]
    }
    "tack-release-up.service" = {
      description = "Start the Tack audit consumer"
      lines = [
        "ExecStart=${local.docker} up -d --wait --wait-timeout 60 --no-deps --no-build audit-consumer",
      ]
    }
    "tack-release-verify.service" = {
      description = "Read audit consumer offsets from Kafka and the Tack ledger"
      lines = [
        "ExecStart=${local.audit_reader_docker} run --rm --no-deps audit-ops ops audit consumer-offsets --execute ${local.identity}",
      ]
    }
  }

  check_units = {
    "tack-release-search-verify.service" = {
      description = "Run ops search verify after the release"
      lines = [
        "ExecStart=${local.docker} run --rm --no-deps tack-ops ops search verify --execute ${local.identity}",
      ]
    }
    "tack-release-dlq-inspect.service" = {
      description = "Run ops audit dlq inspect after the release"
      lines = [
        "ExecStart=${local.audit_reader_docker} run --rm --no-deps audit-ops ops audit dlq inspect --execute ${local.identity}",
      ]
    }
  }

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

resource "pveguest_systemd_unit" "pull" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  name    = "tack-release-pull.service"
  enabled = false
  active  = true

  restart_on = merge(var.configuration_revisions, { unit = pveguest_file.unit["tack-release-pull.service"].write_id })
}

resource "pveguest_systemd_unit" "up" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  name    = "tack-release-up.service"
  enabled = false
  active  = true

  restart_on = merge(var.configuration_revisions, { unit = pveguest_file.unit["tack-release-up.service"].write_id })

  depends_on = [pveguest_systemd_unit.pull]
}

resource "pveguest_systemd_unit" "verify" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  name    = "tack-release-verify.service"
  enabled = false
  active  = true

  restart_on = merge(var.configuration_revisions, { unit = pveguest_file.unit["tack-release-verify.service"].write_id })

  depends_on = [pveguest_systemd_unit.up]
}

resource "pveguest_systemd_unit" "check" {
  for_each = local.check_units

  node = var.node
  vmid = var.vmid
  kind = var.kind

  name    = each.key
  enabled = false
  active  = true

  restart_on = merge(var.configuration_revisions, { unit = pveguest_file.unit[each.key].write_id })

  depends_on = [pveguest_systemd_unit.verify]
}
