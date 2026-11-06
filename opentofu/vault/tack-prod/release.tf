locals {
  tack_release = {
    commit          = "1ad2516cff3bfe4ce49b800dc232340f9151a860"
    server_digest   = "sha256:3dab6897a5286f41d5c92380a2fda8502ae91603c496386e0955846a0ed8dbc4"
    consumer_digest = "sha256:80de4e3bfc766f1edd47cfa42cef13f030c42b0f515985848e7a35e5a011c505"
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
