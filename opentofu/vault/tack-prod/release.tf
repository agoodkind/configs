locals {
  tack_release = {
    commit          = "9518b9dff53a7c8c6f105ff147f5139624c4199a"
    server_digest   = "sha256:6d3624f6ea12283cc78c4ca0dc5aec2e889f7f66261a26f0aef3f52424ab2447"
    consumer_digest = "sha256:68bc41f4d341e76f2e11b252b5d24b5737e53114eb434c20020e629ae784f785"
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
