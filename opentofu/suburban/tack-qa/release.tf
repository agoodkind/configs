locals {
  tack_release = {
    commit          = "139d57f60dbe0fc68f78a003c6779f7705ec2954"
    server_digest   = "sha256:cefbb57dbc0a43f89a39a62765175d6ed50f011add8fdf5b286d250a95216c3f"
    consumer_digest = "sha256:2d8b9d143f18d952654f589cb77153ba3bbae8256d429a7100b88b9152870a46"
    accountable     = "alex@goodkind.io"
  }
}

module "tack_release" {
  source = "../../modules/tack-release"

  node = local.guests["tack_qa_suburban"].node
  vmid = local.guests["tack_qa_suburban"].vmid
  kind = local.guests["tack_qa_suburban"].kind
  role = "owner"

  commit          = local.tack_release.commit
  server_digest   = local.tack_release.server_digest
  consumer_digest = local.tack_release.consumer_digest

  accountable_email = local.tack_release.accountable
}

module "tack_release_app" {
  source = "../../modules/tack-release"

  node = local.guests["tack_app2_suburban"].node
  vmid = local.guests["tack_app2_suburban"].vmid
  kind = local.guests["tack_app2_suburban"].kind
  role = "app"

  commit        = local.tack_release.commit
  server_digest = local.tack_release.server_digest

  accountable_email = local.tack_release.accountable

  depends_on = [module.tack_release]
}

moved {
  from = module.tack_release["tack_qa_suburban"]
  to   = module.tack_release
}

moved {
  from = module.tack_release["tack_app2_suburban"]
  to   = module.tack_release_app
}
