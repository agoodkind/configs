locals {
  tack_release = {
    commit          = "d87e34a8355504c25692bb2edb335abaab86e69d"
    server_digest   = "sha256:322709d72f4111d5d4c2e87cb200cd54befae648c12d9a32caef903496daab3e"
    consumer_digest = "sha256:144fe8beca7863e174eaa1f930fdd1be3670b17dd70a130f0df3e91ef76624f3"
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
  consumer_digest = local.tack_release.consumer_digest

  accountable_email = "alex@goodkind.io"
}
