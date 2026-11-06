locals {
  tack_release = {
    commit          = "aa7e3cc3ba676292a2f9cbfabe09ed33dde210ef"
    server_digest   = "sha256:90f58ac7f282934ffd81f5dff3c4d6b4a066bb449a7f432f4605111ffe1af4f8"
    consumer_digest = "sha256:8efcade513b38c46ffdb5b684e7b3baaf267b7e3cd6be4f823e7dae68d19af9e"
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
