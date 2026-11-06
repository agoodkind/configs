locals {
  tack_release = {
    commit          = "636d531ab7cc6769e8ff769bdb4eb71f4da5cf91"
    server_digest   = "sha256:471acd96d654ecc37fec885b3cc81758eebad4fb23d6b858db15db840648da6d"
    consumer_digest = "sha256:45a42932c1592f06f4d171ea3b463ab4cc342f7ddea13593c8d9332c3fcbff8f"
    accountable     = "alex@goodkind.io"
  }
  tack_release_guests = {
    tack_qa_suburban   = "owner"
    tack_app2_suburban = "app"
  }
}

module "tack_release" {
  source   = "../../modules/tack-release"
  for_each = local.tack_release_guests

  node = local.guests[each.key].node
  vmid = local.guests[each.key].vmid
  kind = local.guests[each.key].kind
  role = each.value

  commit          = local.tack_release.commit
  server_digest   = local.tack_release.server_digest
  consumer_digest = each.value == "owner" ? local.tack_release.consumer_digest : null

  accountable_email = local.tack_release.accountable
}
