locals {
  tack_release = {
    commit          = "573c1da6dde6d4c9dde02fc09fdcfa755dbe9d1f"
    server_digest   = "sha256:67c50b27b83527200dd3f451674e76674d66069fd5e0ad103d0ea00f7f94d42c"
    consumer_digest = "sha256:e3ed2dd2d71fbc37d77b7fbd8b89a30057ec3772fcbdc7fc3f7a0fd5a1246306"
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
