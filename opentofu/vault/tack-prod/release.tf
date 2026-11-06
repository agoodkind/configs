locals {
  tack_release = {
    commit          = "d87e34a8355504c25692bb2edb335abaab86e69d"
    server_digest   = "sha256:322709d72f4111d5d4c2e87cb200cd54befae648c12d9a32caef903496daab3e"
    consumer_digest = "sha256:144fe8beca7863e174eaa1f930fdd1be3670b17dd70a130f0df3e91ef76624f3"
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
