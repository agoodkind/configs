locals {
  backup_check_guest   = local.guests["tack"]
  backup_check_release = module.configuration.backup_check_release
  backup_check_tag     = "${local.backup_check_release.commit}@${local.backup_check_release.server_digest}"
  backup_check_image   = "ghcr.io/agoodkind/tack-server:${local.backup_check_tag}"
}

resource "pveguest_file" "backup_check_image" {
  node = local.backup_check_guest.node
  vmid = local.backup_check_guest.vmid
  kind = local.backup_check_guest.kind
  path = "/etc/systemd/system/tack-backup-staleness.service.d/image.conf"
  content = join("\n", [
    "[Service]",
    "Environment=TACK_IMAGE_TAG=${local.backup_check_tag}",
    "",
  ])
}

resource "pveguest_file" "backup_check_pull_unit" {
  node = local.backup_check_guest.node
  vmid = local.backup_check_guest.vmid
  kind = local.backup_check_guest.kind
  path = "/etc/systemd/system/tack-backup-check-pull.service"
  content = join("\n", [
    "[Unit]",
    "Description=Pull the Tack backup checker image",
    "After=docker.service",
    "Requires=docker.service",
    "",
    "[Service]",
    "Type=oneshot",
    "RemainAfterExit=yes",
    "TimeoutStartSec=280",
    "ExecStart=/usr/bin/docker pull ${local.backup_check_image}",
    "",
  ])
}

resource "pveguest_systemd_unit" "backup_check_pull" {
  node    = local.backup_check_guest.node
  vmid    = local.backup_check_guest.vmid
  kind    = local.backup_check_guest.kind
  name    = "tack-backup-check-pull.service"
  enabled = false
  active  = true

  restart_on = {
    image = pveguest_file.backup_check_image.write_id
    unit  = pveguest_file.backup_check_pull_unit.write_id
  }
}
