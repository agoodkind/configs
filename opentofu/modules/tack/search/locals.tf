locals {
  directory = "/opt/tack-qa"
  guests    = { search = var.search_guest }
  search_secrets = {
    search_ca                = "ca.pem"
    search_certificate       = "node.pem"
    search_private_key       = "node-key.pem"
    search_admin_certificate = "admin.pem"
    search_admin_private_key = "admin-key.pem"
  }
  secret_files = { for key, filename in local.search_secrets : "search/${key}" => { guest = "search", key = key, path = "${local.directory}/search/${filename}" } }
}
