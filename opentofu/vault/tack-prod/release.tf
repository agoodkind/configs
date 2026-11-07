locals {
  tack_release = {
    commit          = "51ef61b2d8287f30188c61ebd8d565dd46b2ea26"
    server_digest   = "sha256:904db2160f2dc415236434a2e1651d8e826f8a43f46f32f2cd25d18a84aaba24"
    consumer_digest = "sha256:ad0ca461cecef643f60971e751a5b7d471d450fd9ff3f2ef6e5a1e0ef79d7301"
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
