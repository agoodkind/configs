locals {
  tack_release = module.configuration.release
}

module "tack_release" {
  source = "../../modules/tack-release"

  node = local.guests["tack"].node
  vmid = local.guests["tack"].vmid
  kind = local.guests["tack"].kind
  role = "owner"

  additional_compose_files = ["docker-compose.k3s-kafka.yml"]
  configuration_revisions = {
    search_endpoint = linux_file_line.compose_search.line
    compose_overlay = pveguest_file.k3s_kafka_overlay.write_id
  }
  health_url = "http://[3d06:bad:b01:0:7ac::2]:8000/healthz"

  commit          = local.tack_release.commit
  server_digest   = local.tack_release.server_digest
  consumer_digest = local.tack_release.consumer_digest

  accountable_email = local.tack_release.accountable

  depends_on = [pveguest_systemd_unit.k3s_kafka_listener]
}

module "tack_release_app" {
  source = "../../modules/tack-release"

  node = local.guests["tack_app2"].node
  vmid = local.guests["tack_app2"].vmid
  kind = local.guests["tack_app2"].kind
  role = "app"

  commit        = local.tack_release.commit
  server_digest = local.tack_release.server_digest

  accountable_email = local.tack_release.accountable

  depends_on = [module.tack_release]
}
