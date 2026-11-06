locals {
  tack_release = {
    commit          = "a0e371c284091ad2112b62344facff38f0545f6d"
    server_digest   = "sha256:ab489cba535677e598a5417553eef4f7bcc1c00ed9c92ecec70c0e554dc246f0"
    consumer_digest = "sha256:eca40420d4f1c5581681c8b8ec08c4c1509a5096211d6df8a4aaef010ec9a0ca"
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
