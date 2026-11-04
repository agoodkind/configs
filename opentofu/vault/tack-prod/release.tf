locals {
  tack_release = {
    commit          = "e31020120360d57709ada3c6c976273e195635a1"
    server_digest   = "sha256:e2531f1dbfe265e0b0c61a5223d0879f4684824aaa91f3c87566ca1976ec0111"
    consumer_digest = "sha256:83c986cd55cd674cd5cdbb8ab6518e13fe50b6f3033e27b7e73fb4548d6f5575"
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
