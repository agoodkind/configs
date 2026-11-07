locals {
  tack_release = {
    commit          = "636d531ab7cc6769e8ff769bdb4eb71f4da5cf91"
    server_digest   = "sha256:471acd96d654ecc37fec885b3cc81758eebad4fb23d6b858db15db840648da6d"
    consumer_digest = "sha256:45a42932c1592f06f4d171ea3b463ab4cc342f7ddea13593c8d9332c3fcbff8f"
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
