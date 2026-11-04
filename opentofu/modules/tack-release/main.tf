# This module updates the Tack app and audit-consumer images on one guest and runs audited checks. Each systemd unit is a oneshot, and pveguest_systemd_unit restarts it when its unit file changes. The module does not change the Compose file, overrides, migrations, or credentials, and a release that changes any of them needs another change. The provider limits each unit restart to 120 seconds, and each unit stops at 115 seconds.

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

variable "owner" {
  description = "When true, selects the guest that runs audit-consumer and the search and audit queue checks."
  type        = bool
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
  description = "Expected tack-audit-consumer index digest."
  type        = string

  validation {
    condition     = can(regex("^sha256:[0-9a-f]{64}$", var.consumer_digest))
    error_message = "Digest must start with `sha256:` followed by exactly 64 lowercase hexadecimal characters."
  }
}

locals {
  install_dir        = "/root/tack"
  docker             = "/usr/bin/docker compose"
  services           = var.owner ? "app audit-consumer" : "app"
  identity           = "--operator-service tack-release --operator-session ${var.commit} --deploy-commit ${var.commit}"
  verify_scope       = var.owner ? "--tack-audit-consumer-digest ${var.consumer_digest}" : "--service app"
  audit_reader_files = "-f docker-compose.yml -f docker-compose.override.yml -f docker-compose.audit-reader.yml"

  stage_units = {
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
        "ExecStart=${local.docker} up -d --no-deps --no-build ${local.services}",
      ]
    }
    "tack-release-verify.service" = {
      description = "Verify running containers use the release images"
      lines = [
        "ExecStart=${local.docker} run --rm tack-ops ops deploy verify --execute --tag ${var.commit} --tack-server-digest ${var.server_digest} ${local.verify_scope} ${local.identity}",
      ]
    }
  }

  check_units = var.owner ? {
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
    "TimeoutStartSec=115",
  ], each.value.lines, [""]))
}

resource "pveguest_systemd_unit" "pull" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  name    = "tack-release-pull.service"
  enabled = false
  active  = true

  restart_on = { unit = pveguest_file.unit["tack-release-pull.service"].write_id }
}

resource "pveguest_systemd_unit" "up" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  name    = "tack-release-up.service"
  enabled = false
  active  = true

  restart_on = { unit = pveguest_file.unit["tack-release-up.service"].write_id }

  depends_on = [pveguest_systemd_unit.pull]
}

resource "pveguest_systemd_unit" "verify" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  name    = "tack-release-verify.service"
  enabled = false
  active  = true

  restart_on = { unit = pveguest_file.unit["tack-release-verify.service"].write_id }

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

  restart_on = { unit = pveguest_file.unit[each.key].write_id }

  depends_on = [pveguest_systemd_unit.verify]
}
