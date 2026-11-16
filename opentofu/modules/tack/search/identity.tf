module "runtime_identity" {
  source   = "../../platform/sysusers"
  guest    = var.search_guest
  identity = var.runtime_identity
}
