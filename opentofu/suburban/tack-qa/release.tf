locals {
  tack_release = {
    commit          = "74576c94ab9ec6662e9634978d72cb6f06de4470"
    server_digest   = "sha256:c588661aca86bc3cb943d231b5eb04a0fed76c02fbd4b9eb44e6ca191db2957e"
    consumer_digest = "sha256:59911e352a6a7012c6b4b95461a4855fb46e11de250a2a210cb623f7e2d0e3e2"
    accountable     = "alex@goodkind.io"
  }
}

module "tack_release" {
  source = "../../modules/tack-release"

  node = local.guests["tack_qa_suburban"].node
  vmid = local.guests["tack_qa_suburban"].vmid
  kind = local.guests["tack_qa_suburban"].kind
  role = "owner"

  additional_compose_files = ["docker-compose.k3s-kafka.yml"]
  health_url               = "http://[3d06:bad:b01:210:7ac::2]:8000/healthz"

  commit          = local.tack_release.commit
  server_digest   = local.tack_release.server_digest
  consumer_digest = local.tack_release.consumer_digest

  accountable_email = local.tack_release.accountable

  depends_on = [pveguest_systemd_unit.k3s_kafka_listener]
}

module "tack_release_app" {
  source = "../../modules/tack-release"

  node = local.guests["tack_app2_suburban"].node
  vmid = local.guests["tack_app2_suburban"].vmid
  kind = local.guests["tack_app2_suburban"].kind
  role = "app"

  commit        = local.tack_release.commit
  server_digest = local.tack_release.server_digest

  accountable_email = local.tack_release.accountable

  depends_on = [module.tack_release, pveguest_systemd_unit.legacy_app_kafka]
}

moved {
  from = module.tack_release["tack_qa_suburban"]
  to   = module.tack_release
}

moved {
  from = module.tack_release["tack_app2_suburban"]
  to   = module.tack_release_app
}
