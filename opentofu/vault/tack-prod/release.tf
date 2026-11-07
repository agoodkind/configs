locals {
  tack_release = {
    commit          = "7e469f9851c55e0ae36a03fe279287c6a430da05"
    server_digest   = "sha256:bf9ae16d37395ef5bf15459a4bc8430b42e79943932bf176e092ac844930cabd"
    consumer_digest = "sha256:0b2c3175d3e4e1a3c86979f585d0e0fb57c430b23f9ea6618373309b1db9114b"
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
