locals {
  tack_release = {
    commit          = "03b0caa396e9c559ce958f6609f40cd6fa531762"
    server_digest   = "sha256:c7574033ebe2b6269403bd000f47831d7e099cb290c6dbccb36e7750bd663f06"
    consumer_digest = "sha256:40c13b306d9a3fe1acd0b0fbaafbd6066c2f2f5694689b4220c3fba3e530214c"
    accountable     = "alex@goodkind.io"
  }
}

module "tack_release" {
  source = "../../modules/tack-release"

  node = local.guests["tack"].node
  vmid = local.guests["tack"].vmid
  kind = local.guests["tack"].kind
  role = "owner"

  commit          = local.tack_release.commit
  server_digest   = local.tack_release.server_digest
  consumer_digest = local.tack_release.consumer_digest

  accountable_email = local.tack_release.accountable
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
