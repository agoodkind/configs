locals {
  tack_release = {
    commit          = "1ad2516cff3bfe4ce49b800dc232340f9151a860"
    server_digest   = "sha256:3dab6897a5286f41d5c92380a2fda8502ae91603c496386e0955846a0ed8dbc4"
    consumer_digest = "sha256:80de4e3bfc766f1edd47cfa42cef13f030c42b0f515985848e7a35e5a011c505"
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
