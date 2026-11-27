locals {
  tack_release    = module.configuration.release
  server_image    = "ghcr.io/agoodkind/tack-server@${local.tack_release.server_digest}"
  release_compose = [basename(pveguest_file.k3s_kafka_overlay.path), basename(pveguest_file.release_images.path)]
}

resource "pveguest_file" "release_images" {
  node = local.k3s_kafka_guest.node
  vmid = local.k3s_kafka_guest.vmid
  kind = local.k3s_kafka_guest.kind
  path = "/root/tack/docker-compose.release.yml"
  content = yamlencode({
    services = {
      app            = { image = local.server_image }
      audit-ops      = { image = local.server_image }
      audit-consumer = { image = "ghcr.io/agoodkind/tack-audit-consumer@${local.tack_release.consumer_digest}" }
      tack-ops       = { image = "$${TACK_OPS_IMAGE:-${local.server_image}}" }
    }
  })
}

resource "linux_file_line" "compose_files" {
  provider   = linux.compose
  path       = "/root/tack/.env"
  match      = "^COMPOSE_FILE="
  line       = "COMPOSE_FILE=${join(":", concat(["docker-compose.yml", "docker-compose.override.yml"], local.release_compose))}"
  depends_on = [linux_file_line.compose_search, pveguest_systemd_unit.backup_check_pull]
}

resource "linux_file_line" "compose_image_tag" {
  provider   = linux.compose
  path       = "/root/tack/.env"
  match      = "^TACK_IMAGE_TAG="
  line       = "TACK_IMAGE_TAG=${local.tack_release.commit}"
  depends_on = [linux_file_line.compose_files]
}

module "tack_release" {
  source = "../../modules/tack-release"

  node                     = local.guests["tack"].node
  vmid                     = local.guests["tack"].vmid
  kind                     = local.guests["tack"].kind
  additional_compose_files = local.release_compose
  configuration_revisions = {
    search_endpoint = linux_file_line.compose_search.line
    image_tag       = linux_file_line.compose_image_tag.line
    images          = pveguest_file.release_images.write_id
  }

  commit            = local.tack_release.commit
  accountable_email = local.tack_release.accountable

  depends_on = [pveguest_systemd_unit.k3s_kafka_listener]
}
