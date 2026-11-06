locals {
  tack_release = {
    commit          = "a0e371c284091ad2112b62344facff38f0545f6d"
    server_digest   = "sha256:ab489cba535677e598a5417553eef4f7bcc1c00ed9c92ecec70c0e554dc246f0"
    consumer_digest = "sha256:eca40420d4f1c5581681c8b8ec08c4c1509a5096211d6df8a4aaef010ec9a0ca"
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
